package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// SearchDeliveryRecords returns the requested page of records that satisfy
// every filter in the query, ordered by occurrence time then id, plus the
// total number of matching records across all pages. It only reads already
// registered records.
func (s *Store) SearchDeliveryRecords(ctx context.Context, query delivery.SearchQuery) ([]delivery.Record, int, error) {
	where := []string{"channel = ?", "occurred_at >= ?", "occurred_at < ?"}
	args := []any{
		query.Channel,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	}
	if query.HasTemplateID {
		where = append(where, "template_id = ?")
		args = append(args, query.TemplateID)
	}
	if query.HasStatus {
		where = append(where, "status = ?")
		args = append(args, query.Status)
	}
	if query.HasRetryMin {
		where = append(where, "retry_count >= ?")
		args = append(args, query.RetryMin)
	}
	if query.HasRetryMax {
		where = append(where, "retry_count <= ?")
		args = append(args, query.RetryMax)
	}
	if query.HasFailureReasonContains {
		// instr does a binary, case-sensitive substring check; LIKE would be
		// case-insensitive for ASCII under the default pragma.
		where = append(where, "instr(failure_reason, ?) > 0")
		args = append(args, query.FailureReasonContains)
	}
	condition := strings.Join(where, " AND ")

	var total int
	countArgs := append([]any(nil), args...)
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM delivery_records WHERE "+condition, countArgs...,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	pageArgs := append(append([]any(nil), args...),
		query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason
		   FROM delivery_records
		  WHERE `+condition+`
		  ORDER BY occurred_at ASC, id ASC
		  LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	var records []delivery.Record
	for rows.Next() {
		record, err := scanDeliveryRecord(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return records, total, nil
}
