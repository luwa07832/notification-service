package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// ChannelComparison returns read-only per-channel aggregates for the requested
// channels over the validated half-open [start, end) range. Only existing rows
// are read: registration, retry handling and stored failure reasons stay
// untouched, and no missing attempts are ever fabricated.
func (s *Store) ChannelComparison(ctx context.Context, query delivery.ComparisonQuery) (delivery.ChannelComparison, error) {
	clauses := []string{
		"occurred_at >= ?",
		"occurred_at < ?",
		"channel IN (" + placeholders(len(query.Channels)) + ")",
	}
	args := []any{
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	}
	for _, channel := range query.Channels {
		args = append(args, channel)
	}
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

	totals := make(map[string]int, len(query.Channels))
	statusCounts := make(map[string]delivery.StatusCounts, len(query.Channels))
	retryMin := make(map[string]*int, len(query.Channels))
	retryMax := make(map[string]*int, len(query.Channels))
	failureCounts := map[string]map[string]int{}
	for _, channel := range query.Channels {
		totals[channel] = 0
		statusCounts[channel] = delivery.StatusCounts{}
		failureCounts[channel] = map[string]int{}
	}

	for rows.Next() {
		var channel, status, failureReason string
		var retryCount int
		if err := rows.Scan(&channel, &status, &retryCount, &failureReason); err != nil {
			return delivery.ChannelComparison{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		totals[channel]++
		counts := statusCounts[channel]
		switch status {
		case delivery.StatusPending:
			counts.Pending++
		case delivery.StatusRetrying:
			counts.Retrying++
		case delivery.StatusSucceeded:
			counts.Succeeded++
		case delivery.StatusFailed:
			counts.Failed++
		}
		statusCounts[channel] = counts
		if min, ok := retryMin[channel]; !ok || retryCount < *min {
			value := retryCount
			retryMin[channel] = &value
		}
		if max, ok := retryMax[channel]; !ok || retryCount > *max {
			value := retryCount
			retryMax[channel] = &value
		}
		if (status == delivery.StatusFailed || status == delivery.StatusRetrying) && failureReason != "" {
			failureCounts[channel][failureReason]++
		}
	}
	if err := rows.Err(); err != nil {
		return delivery.ChannelComparison{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	reasonTotals := map[string]int{}
	for _, channel := range query.Channels {
		for reason, count := range failureCounts[channel] {
			reasonTotals[reason] += count
		}
	}
	reasons := make([]string, 0, len(reasonTotals))
	for reason := range reasonTotals {
		reasons = append(reasons, reason)
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasonTotals[reasons[i]] != reasonTotals[reasons[j]] {
			return reasonTotals[reasons[i]] > reasonTotals[reasons[j]]
		}
		return reasons[i] < reasons[j]
	})

	failureReasons := make([]delivery.ComparisonFailureReason, 0, len(reasons))
	for _, reason := range reasons {
		channelCounts := make(map[string]int, len(query.Channels))
		for _, channel := range query.Channels {
			channelCounts[channel] = failureCounts[channel][reason]
		}
		failureReasons = append(failureReasons, delivery.ComparisonFailureReason{
			FailureReason: reason,
			ChannelCounts: channelCounts,
			AttemptCount:  reasonTotals[reason],
		})
	}

	return delivery.ChannelComparison{
		Channels:       query.Channels,
		TotalAttempts:  totals,
		StatusCounts:   statusCounts,
		RetryCountMin:  retryMin,
		RetryCountMax:  retryMax,
		FailureReasons: failureReasons,
	}, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
