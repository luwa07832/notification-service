package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// AlignmentResult is one page of template-alignment issues together with the
// exact number of mismatching records before pagination.
type AlignmentResult struct {
	Issues []delivery.AlignmentIssue
	Total  int
}

const alignmentIssueExpr = `CASE
		WHEN nt.template_id IS NULL THEN 'template_missing'
		WHEN NOT EXISTS (SELECT 1 FROM json_each(nt.channels) WHERE value = dr.channel) THEN 'channel_unsupported'
		WHEN nt.enabled = 0 THEN 'template_disabled'
	END`

// TemplateAlignment compares registered delivery records in the queried
// channel and half-open time range against the current notification_templates
// registration. It only reads existing rows: no record is written and no
// historical failure reason is changed.
func (s *Store) TemplateAlignment(ctx context.Context, query delivery.AlignmentQuery) (AlignmentResult, error) {
	clauses := []string{
		"dr.channel = ?",
		"dr.occurred_at >= ?",
		"dr.occurred_at < ?",
		alignmentIssueExpr + " IS NOT NULL",
	}
	args := []any{
		query.Channel,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	}
	if query.TemplateID != nil {
		clauses = append(clauses, "dr.template_id = ?")
		args = append(args, *query.TemplateID)
	}
	filter := strings.Join(clauses, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM delivery_records dr
		   LEFT JOIN notification_templates nt ON nt.template_id = dr.template_id
		  WHERE `+filter,
		args...,
	).Scan(&total); err != nil {
		return AlignmentResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	limit := query.PageSize
	offset := (query.Page - 1) * query.PageSize
	pageArgs := make([]any, 0, len(args)+2)
	pageArgs = append(pageArgs, args...)
	pageArgs = append(pageArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT dr.id, dr.template_id, dr.channel, dr.occurred_at, dr.status, dr.retry_count, dr.failure_reason,
		        `+alignmentIssueExpr+`
		   FROM delivery_records dr
		   LEFT JOIN notification_templates nt ON nt.template_id = dr.template_id
		  WHERE `+filter+`
		  ORDER BY dr.occurred_at ASC, dr.id ASC
		  LIMIT ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		return AlignmentResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	issues := []delivery.AlignmentIssue{}
	for rows.Next() {
		var (
			issue      delivery.AlignmentIssue
			occurredAt string
		)
		if err := rows.Scan(
			&issue.ID, &issue.TemplateID, &issue.Channel, &occurredAt,
			&issue.Status, &issue.RetryCount, &issue.FailureReason, &issue.IssueCode,
		); err != nil {
			return AlignmentResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		parsedAt, err := time.Parse(storedTimeFormat, occurredAt)
		if err != nil {
			return AlignmentResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		issue.OccurredAt = parsedAt.UTC()
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return AlignmentResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return AlignmentResult{Issues: issues, Total: total}, nil
}
