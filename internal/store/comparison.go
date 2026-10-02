package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// ChannelComparison aggregates registered attempts across the validated
// channels for the half-open [start, end) range. Every requested channel is
// present in the result even when it has no attempt, in which case its retry
// bounds stay nil. Failure reasons are the stored text verbatim for failed or
// retrying records only, ordered by total attempt count descending and then
// by the original reason ascending. The scan only reads existing rows:
// registration, retry handling and stored failure reasons stay untouched,
// and no missing attempts or failure reasons are ever fabricated.
func (s *Store) ChannelComparison(ctx context.Context, query delivery.ComparisonQuery) (delivery.ChannelComparison, error) {
	placeholders := make([]string, len(query.Channels))
	channelArgs := make([]any, len(query.Channels))
	for i, channel := range query.Channels {
		placeholders[i] = "?"
		channelArgs[i] = channel
	}

	clauses := []string{
		"channel IN (" + strings.Join(placeholders, ", ") + ")",
		"occurred_at >= ?",
		"occurred_at < ?",
	}
	args := append(channelArgs,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	)
	if query.TemplateID != nil {
		clauses = append(clauses, "template_id = ?")
		args = append(args, *query.TemplateID)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT channel, status, retry_count, failure_reason
		   FROM delivery_records
		  WHERE `+strings.Join(clauses, " AND "),
		args...,
	)
	if err != nil {
		return delivery.ChannelComparison{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	stats := make(map[string]*delivery.ComparisonChannel, len(query.Channels))
	totalAttempts := make(map[string]int, len(query.Channels))
	statusCounts := make(map[string]delivery.StatusCounts, len(query.Channels))
	retryMin := make(map[string]*int, len(query.Channels))
	retryMax := make(map[string]*int, len(query.Channels))
	for _, channel := range query.Channels {
		stats[channel] = &delivery.ComparisonChannel{}
	}

	failureCounts := map[string]map[string]int{}
	for rows.Next() {
		var channel, status, failureReason string
		var retryCount int
		if err := rows.Scan(&channel, &status, &retryCount, &failureReason); err != nil {
			return delivery.ChannelComparison{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		channelStats := stats[channel]
		if channelStats == nil {
			continue
		}
		channelStats.TotalAttempts++
		if channelStats.RetryCountMin == nil || retryCount < *channelStats.RetryCountMin {
			channelStats.RetryCountMin = intPtr(retryCount)
		}
		if channelStats.RetryCountMax == nil || retryCount > *channelStats.RetryCountMax {
			channelStats.RetryCountMax = intPtr(retryCount)
		}
		switch status {
		case delivery.StatusPending:
			channelStats.StatusCounts.Pending++
		case delivery.StatusRetrying:
			channelStats.StatusCounts.Retrying++
		case delivery.StatusSucceeded:
			channelStats.StatusCounts.Succeeded++
		case delivery.StatusFailed:
			channelStats.StatusCounts.Failed++
		}
		if (status == delivery.StatusFailed || status == delivery.StatusRetrying) && failureReason != "" {
			if failureCounts[failureReason] == nil {
				failureCounts[failureReason] = map[string]int{}
			}
			failureCounts[failureReason][channel]++
		}
	}
	if err := rows.Err(); err != nil {
		return delivery.ChannelComparison{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	for _, channel := range query.Channels {
		channelStats := stats[channel]
		totalAttempts[channel] = channelStats.TotalAttempts
		statusCounts[channel] = channelStats.StatusCounts
		retryMin[channel] = channelStats.RetryCountMin
		retryMax[channel] = channelStats.RetryCountMax
	}

	reasons := make([]delivery.ComparisonFailureReason, 0, len(failureCounts))
	for reason, perChannel := range failureCounts {
		channelCounts := make(map[string]int, len(query.Channels))
		total := 0
		for _, channel := range query.Channels {
			count := perChannel[channel]
			channelCounts[channel] = count
			total += count
		}
		reasons = append(reasons, delivery.ComparisonFailureReason{
			FailureReason: reason,
			ChannelCounts: channelCounts,
			AttemptCount:  total,
		})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].AttemptCount != reasons[j].AttemptCount {
			return reasons[i].AttemptCount > reasons[j].AttemptCount
		}
		return reasons[i].FailureReason < reasons[j].FailureReason
	})

	return delivery.ChannelComparison{
		Channels:       append([]string(nil), query.Channels...),
		TotalAttempts:  totalAttempts,
		StatusCounts:   statusCounts,
		RetryCountMin:  retryMin,
		RetryCountMax:  retryMax,
		FailureReasons: reasons,
	}, nil
}

func intPtr(value int) *int { return &value }
