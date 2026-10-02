package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// AlignmentResult is one page of misaligned delivery records together with
// the exact total number of mismatching records before pagination.
type AlignmentResult struct {
	Issues []delivery.AlignmentIssue
	Total  int
}

// alignmentIssueSelect computes the single issue code of a record against the
// current template registry. The LEFT JOIN leaves missing templates with a
// NULL template side, and the CASE keeps the published priority: a missing
// template beats an unsupported channel, which beats a disabled template
// that does support the record channel. Consistent records get NULL and are
// filtered out by the outer query. The issue codes are bound from the
// delivery package so they stay defined in one place.
const alignmentIssueSelect = `r.id, r.template_id, r.channel, r.occurred_at, r.status, r.retry_count, r.failure_reason,
		CASE
			WHEN t.template_id IS NULL THEN ?
			WHEN NOT EXISTS (SELECT 1 FROM json_each(t.channels) WHERE value = r.channel) THEN ?
			WHEN t.enabled = 0 THEN ?
			ELSE NULL
		END AS issue_code`

// TemplateAlignment returns one page of registered delivery records whose
// channel does not match the current notification_templates registry. It only
// reads existing rows: records and their stored failure reasons, and the
// registry itself, are never modified.
func (s *Store) TemplateAlignment(ctx context.Context, query delivery.AlignmentQuery) (AlignmentResult, error) {
	clauses := []string{
		"r.channel = ?",
		"r.occurred_at >= ?",
		"r.occurred_at < ?",
	}
	args := []any{
		delivery.IssueTemplateMissing,
		delivery.IssueChannelUnsupported,
		delivery.IssueTemplateDisabled,
		query.Channel,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	}
	if query.TemplateID != nil {
		clauses = append(clauses, "r.template_id = ?")
		args = append(args, *query.TemplateID)
	}
	inner := `SELECT ` + alignmentIssueSelect + `
	            FROM delivery_records r
	            LEFT JOIN notification_templates t ON t.template_id = r.template_id
	           WHERE ` + strings.Join(clauses, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (`+inner+`) WHERE issue_code IS NOT NULL`,
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
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason, issue_code
		   FROM (`+inner+`)
		  WHERE issue_code IS NOT NULL
		  ORDER BY occurred_at ASC, id ASC
		  LIMIT ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		return AlignmentResult{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	issues := []delivery.AlignmentIssue{}
	for rows.Next() {
		var issue delivery.AlignmentIssue
		var occurredAt string
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
