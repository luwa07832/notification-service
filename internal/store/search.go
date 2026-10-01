package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// SearchResult is one page of an advanced search together with the exact
// total number of records matching every supplied condition.
type SearchResult struct {
	Records []delivery.Record
	Total   int
}

// SearchDeliveryRecords returns one page of registered records matching the
// validated search. Matching only reads existing rows: registration, retry
// handling and stored failure reasons stay untouched.
func (s *Store) SearchDeliveryRecords(ctx context.Context, search delivery.SearchQuery) (SearchResult, error) {
	clauses := []string{
		"channel = ?",
		"occurred_at >= ?",
		"occurred_at < ?",
	}
	args := []any{
		search.Channel,
		search.Start.UTC().Format(storedTimeFormat),
		search.End.UTC().Format(storedTimeFormat),
	}
	if search.TemplateID != nil {
		clauses = append(clauses, "template_id = ?")
		args = append(args, *search.TemplateID)
	}
	if search.Status != nil {
		clauses = append(clauses, "status = ?")
		args = append(args, *search.Status)
	}
	if search.RetryMin != nil {
		clauses = append(clauses, "retry_count >= ?")
		args = append(args, *search.RetryMin)
	}
	if search.RetryMax != nil {
		clauses = append(clauses, "retry_count <= ?")
		args = append(args, *search.RetryMax)
	}
	if search.FailureReasonContain != nil {
		clauses = append(clauses, "instr(failure_reason, ?) > 0")
		args = append(args, *search.FailureReasonContain)
	}
	filter := strings.Join(clauses, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM delivery_records WHERE `+filter,
		args...,
	).Scan(&total); err != nil {
		return SearchResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	limit := search.PageSize
	offset := (search.Page - 1) * search.PageSize
	pageArgs := make([]any, 0, len(args)+2)
	pageArgs = append(pageArgs, args...)
	pageArgs = append(pageArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason
		   FROM delivery_records
		  WHERE `+filter+`
		  ORDER BY occurred_at ASC, id ASC
		  LIMIT ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		return SearchResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	records := []delivery.Record{}
	for rows.Next() {
		record, err := scanDeliveryRecord(rows)
		if err != nil {
			return SearchResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return SearchResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return SearchResult{Records: records, Total: total}, nil
}
