package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// FailureSummaryGroup is one exact stored failure_reason text together with
// the number of matching attempts. The text is grouped and ordered as stored:
// no case folding, trimming or normalization is applied.
type FailureSummaryGroup struct {
	FailureReason string `json:"failure_reason"`
	AttemptCount  int    `json:"attempt_count"`
}

// FailureSummary is the read-only aggregation over registered records.
// TotalAttempts is the sum of every group count.
type FailureSummary struct {
	Groups        []FailureSummaryGroup `json:"groups"`
	TotalAttempts int                   `json:"total_attempts"`
}

// SummarizeDeliveryFailures counts failed and retrying attempts that carry a
// non-empty failure reason, grouped by the exact stored reason text. Groups
// sort by attempt count descending, ties by the reason text ascending. The
// query only reads existing rows: registration, retry handling and stored
// failure reasons stay untouched.
func (s *Store) SummarizeDeliveryFailures(ctx context.Context, query delivery.SummaryQuery) (FailureSummary, error) {
	clauses := []string{
		"channel = ?",
		"occurred_at >= ?",
		"occurred_at < ?",
		"status IN (?, ?)",
		"failure_reason != ''",
	}
	args := []any{
		query.Channel,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
		delivery.StatusFailed,
		delivery.StatusRetrying,
	}
	if query.TemplateID != nil {
		clauses = append(clauses, "template_id = ?")
		args = append(args, *query.TemplateID)
	}
	filter := strings.Join(clauses, " AND ")

	rows, err := s.db.QueryContext(ctx,
		`SELECT failure_reason, COUNT(*)
		   FROM delivery_records
		  WHERE `+filter+`
		  GROUP BY failure_reason
		  ORDER BY COUNT(*) DESC, failure_reason ASC`,
		args...,
	)
	if err != nil {
		return FailureSummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	summary := FailureSummary{Groups: []FailureSummaryGroup{}}
	for rows.Next() {
		var group FailureSummaryGroup
		if err := rows.Scan(&group.FailureReason, &group.AttemptCount); err != nil {
			return FailureSummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		summary.Groups = append(summary.Groups, group)
		summary.TotalAttempts += group.AttemptCount
	}
	if err := rows.Err(); err != nil {
		return FailureSummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return summary, nil
}
