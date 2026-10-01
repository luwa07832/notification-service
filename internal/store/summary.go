package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// FailureSummary groups failed or retrying attempts by their exact, stored
// failure reason. TotalAttempts is the sum of the per-group counts.
type FailureSummary struct {
	Groups        []delivery.FailureGroup
	TotalAttempts int
}

// FailureSummary returns read-only failure-reason counts for records matching
// the validated query. Only failed or retrying records with a non-empty
// stored failure reason participate, and the stored text is grouped verbatim:
// no case folding, trimming or other normalization is applied. Groups are
// ordered by attempt count descending and then by the original failure reason
// ascending. Registration and stored history are never modified.
func (s *Store) FailureSummary(ctx context.Context, query delivery.SummaryQuery) (FailureSummary, error) {
	clauses := []string{
		"channel = ?",
		"occurred_at >= ?",
		"occurred_at < ?",
		"status IN (?, ?)",
		"failure_reason <> ''",
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

	rows, err := s.db.QueryContext(ctx,
		`SELECT failure_reason, COUNT(*)
		   FROM delivery_records
		  WHERE `+strings.Join(clauses, " AND ")+`
		  GROUP BY failure_reason
		  ORDER BY COUNT(*) DESC, failure_reason ASC`,
		args...,
	)
	if err != nil {
		return FailureSummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	groups := []delivery.FailureGroup{}
	total := 0
	for rows.Next() {
		var group delivery.FailureGroup
		if err := rows.Scan(&group.FailureReason, &group.AttemptCount); err != nil {
			return FailureSummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		groups = append(groups, group)
		total += group.AttemptCount
	}
	if err := rows.Err(); err != nil {
		return FailureSummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return FailureSummary{Groups: groups, TotalAttempts: total}, nil
}
