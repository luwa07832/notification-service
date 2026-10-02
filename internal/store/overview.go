package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// AttemptOverview is the read-only per-template delivery overview for one
// channel and half-open time range.
type AttemptOverview struct {
	Groups []delivery.OverviewGroup
}

// AttemptOverview aggregates the registered records matching the validated
// query by template, ordered by template identifier ascending. The query only
// reads existing rows: registration, retry handling and stored failure
// reasons stay untouched, and no attempt is backfilled.
func (s *Store) AttemptOverview(ctx context.Context, query delivery.OverviewQuery) (AttemptOverview, error) {
	clauses := []string{
		"channel = ?",
		"occurred_at >= ?",
		"occurred_at < ?",
	}
	args := []any{
		query.Channel,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	}
	if query.TemplateID != nil {
		clauses = append(clauses, "template_id = ?")
		args = append(args, *query.TemplateID)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, template_id, occurred_at, status, retry_count, failure_reason
		   FROM delivery_records
		  WHERE `+strings.Join(clauses, " AND ")+`
		  ORDER BY template_id ASC, occurred_at ASC, id ASC`,
		args...,
	)
	if err != nil {
		return AttemptOverview{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	groups := []delivery.OverviewGroup{}
	failureCounts := map[string]int{}
	current := -1
	for rows.Next() {
		var (
			id, templateID, occurredAt, status, failureReason string
			retryCount                                        int
		)
		if err := rows.Scan(&id, &templateID, &occurredAt, &status, &retryCount, &failureReason); err != nil {
			return AttemptOverview{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		parsedAt, err := time.Parse(storedTimeFormat, occurredAt)
		if err != nil {
			return AttemptOverview{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}

		if current < 0 || groups[current].TemplateID != templateID {
			if current >= 0 {
				groups[current].FailureReasons = sortedFailureGroups(failureCounts)
			}
			groups = append(groups, delivery.OverviewGroup{
				TemplateID:    templateID,
				RetryCountMin: retryCount,
				RetryCountMax: retryCount,
			})
			failureCounts = map[string]int{}
			current++
		}
		group := &groups[current]
		group.TotalAttempts++
		if retryCount < group.RetryCountMin {
			group.RetryCountMin = retryCount
		}
		if retryCount > group.RetryCountMax {
			group.RetryCountMax = retryCount
		}
		switch status {
		case delivery.StatusPending:
			group.StatusCounts.Pending++
		case delivery.StatusRetrying:
			group.StatusCounts.Retrying++
		case delivery.StatusSucceeded:
			group.StatusCounts.Succeeded++
		case delivery.StatusFailed:
			group.StatusCounts.Failed++
		}
		// Rows arrive ordered by occurred_at and then id within one template,
		// so the row scanned last is exactly the required last attempt.
		group.LastAttempt = delivery.LastAttempt{
			ID:            id,
			OccurredAt:    parsedAt.UTC(),
			Status:        status,
			RetryCount:    retryCount,
			FailureReason: failureReason,
		}
		if (status == delivery.StatusFailed || status == delivery.StatusRetrying) && failureReason != "" {
			failureCounts[failureReason]++
		}
	}
	if err := rows.Err(); err != nil {
		return AttemptOverview{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if current >= 0 {
		groups[current].FailureReasons = sortedFailureGroups(failureCounts)
	}
	return AttemptOverview{Groups: groups}, nil
}

// sortedFailureGroups turns verbatim failure-reason counts into groups ordered
// by attempt count descending and then by the original reason text ascending.
// The result is never nil so it serializes as an empty JSON array.
func sortedFailureGroups(counts map[string]int) []delivery.FailureGroup {
	groups := make([]delivery.FailureGroup, 0, len(counts))
	for reason, count := range counts {
		groups = append(groups, delivery.FailureGroup{FailureReason: reason, AttemptCount: count})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].AttemptCount != groups[j].AttemptCount {
			return groups[i].AttemptCount > groups[j].AttemptCount
		}
		return groups[i].FailureReason < groups[j].FailureReason
	})
	return groups
}
