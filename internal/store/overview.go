package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// AttemptOverview aggregates registered attempts per template for one channel
// and the half-open [start, end) range. Groups are ordered by template
// identifier ascending. The scan only reads existing rows: registration,
// retry handling and stored failure reasons stay untouched, and no missing
// attempts are ever fabricated.
func (s *Store) AttemptOverview(ctx context.Context, query delivery.OverviewQuery) ([]delivery.OverviewGroup, error) {
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
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	order := []string{}
	byTemplate := map[string]*delivery.OverviewGroup{}
	failureCounts := map[string]map[string]int{}
	for rows.Next() {
		var (
			id, templateID, occurredAt string
			status, failureReason      string
			retryCount                 int
		)
		if err := rows.Scan(&id, &templateID, &occurredAt, &status, &retryCount, &failureReason); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		parsedAt, err := time.Parse(storedTimeFormat, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}

		group, ok := byTemplate[templateID]
		if !ok {
			group = &delivery.OverviewGroup{
				TemplateID:     templateID,
				FailureReasons: []delivery.FailureGroup{},
			}
			byTemplate[templateID] = group
			failureCounts[templateID] = map[string]int{}
			order = append(order, templateID)
		}

		group.TotalAttempts++
		if group.TotalAttempts == 1 || retryCount < group.RetryCountMin {
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
		// Rows arrive in (occurred_at, id) ascending order per template, so
		// the last row seen is exactly the greatest occurred_at and id.
		group.LastAttempt = delivery.OverviewLastAttempt{
			ID:            id,
			OccurredAt:    parsedAt.UTC(),
			Status:        status,
			RetryCount:    retryCount,
			FailureReason: failureReason,
		}
		if (status == delivery.StatusFailed || status == delivery.StatusRetrying) && failureReason != "" {
			failureCounts[templateID][failureReason]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	groups := make([]delivery.OverviewGroup, 0, len(order))
	for _, templateID := range order {
		group := byTemplate[templateID]
		counts := failureCounts[templateID]
		reasons := make([]delivery.FailureGroup, 0, len(counts))
		for reason, count := range counts {
			reasons = append(reasons, delivery.FailureGroup{FailureReason: reason, AttemptCount: count})
		}
		sort.Slice(reasons, func(i, j int) bool {
			if reasons[i].AttemptCount != reasons[j].AttemptCount {
				return reasons[i].AttemptCount > reasons[j].AttemptCount
			}
			return reasons[i].FailureReason < reasons[j].FailureReason
		})
		group.FailureReasons = reasons
		groups = append(groups, *group)
	}
	return groups, nil
}
