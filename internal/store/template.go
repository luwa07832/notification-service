package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/template"

	sqlite "modernc.org/sqlite"
)

// sqliteConstraint is the primary result code for a constraint violation.
// Extended codes keep the constraint byte in their low eight bits.
const sqliteConstraint = 19

// ErrTemplateAlreadyExists marks a registration whose template_id is already
// taken. The HTTP layer maps it to the published TEMPLATE_ALREADY_EXISTS
// response.
var ErrTemplateAlreadyExists = errors.New("notification template already exists")

// CreateNotificationTemplate stores one immutable template together with its
// supported channels in request order.
func (s *Store) CreateNotificationTemplate(ctx context.Context, tpl template.Template) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO notification_templates (template_id, name, body, enabled)
		  VALUES (?, ?, ?, ?)`,
		tpl.TemplateID, tpl.Name, tpl.Body, tpl.Enabled,
	)
	if err != nil {
		if isConstraintError(err) {
			return ErrTemplateAlreadyExists
		}
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	for position, channel := range tpl.Channels {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO notification_template_channels (template_id, channel, position)
			  VALUES (?, ?, ?)`,
			tpl.TemplateID, channel, position,
		)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
	}

	if err := tx.Commit(); err != nil {
		if isConstraintError(err) {
			return ErrTemplateAlreadyExists
		}
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return nil
}

// ListNotificationTemplates returns registered templates matching the exact
// channel and enabled filters, ordered by template_id ascending. Each result
// carries its channels in registration order.
func (s *Store) ListNotificationTemplates(ctx context.Context, filter template.ListFilter) ([]template.Template, error) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if filter.Channel != nil {
		clauses = append(clauses,
			`EXISTS (SELECT 1 FROM notification_template_channels c
			          WHERE c.template_id = t.template_id AND c.channel = ?)`)
		args = append(args, *filter.Channel)
	}
	if filter.Enabled != nil {
		clauses = append(clauses, "t.enabled = ?")
		args = append(args, *filter.Enabled)
	}
	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT t.template_id, t.name, t.body, t.enabled
		   FROM notification_templates t
		  `+where+`
		  ORDER BY t.template_id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	var templates []template.Template
	var ids []string
	for rows.Next() {
		var tpl template.Template
		if err := rows.Scan(&tpl.TemplateID, &tpl.Name, &tpl.Body, &tpl.Enabled); err != nil {
			rows.Close()
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		templates = append(templates, tpl)
		ids = append(ids, tpl.TemplateID)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	channelsByID, err := s.channelsForTemplates(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range templates {
		templates[i].Channels = channelsByID[templates[i].TemplateID]
	}
	return templates, nil
}

// GetNotificationTemplateDetail loads one template by id and aggregates every
// registered delivery attempt for it. The boolean is false when no template
// exists; that is not a storage failure.
func (s *Store) GetNotificationTemplateDetail(ctx context.Context, templateID string) (template.Detail, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT template_id, name, body, enabled
		   FROM notification_templates WHERE template_id = ?`, templateID)

	var tpl template.Template
	if err := row.Scan(&tpl.TemplateID, &tpl.Name, &tpl.Body, &tpl.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return template.Detail{}, false, nil
		}
		return template.Detail{}, false, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	channelsByID, err := s.channelsForTemplates(ctx, []string{templateID})
	if err != nil {
		return template.Detail{}, false, err
	}
	tpl.Channels = channelsByID[templateID]

	summary, err := s.templateDeliverySummary(ctx, templateID)
	if err != nil {
		return template.Detail{}, false, err
	}
	return template.Detail{Template: tpl, DeliverySummary: summary}, true, nil
}

// channelsForTemplates returns the channels of every requested template keyed
// by template id, each list in registration (submission) order.
func (s *Store) channelsForTemplates(ctx context.Context, ids []string) (map[string][]string, error) {
	channels := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return channels, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT template_id, channel FROM notification_template_channels
		  WHERE template_id IN (`+strings.Join(placeholders, ", ")+`)
		  ORDER BY template_id ASC, rowid ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, channel string
		if err := rows.Scan(&id, &channel); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		channels[id] = append(channels[id], channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return channels, nil
}

// templateDeliverySummary aggregates every registered delivery attempt of one
// template. Failure reasons are the stored text verbatim for failed or
// retrying records only, ordered by count descending and then by the original
// reason ascending.
func (s *Store) templateDeliverySummary(ctx context.Context, templateID string) (template.DeliverySummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, retry_count, failure_reason FROM delivery_records
		  WHERE template_id = ?`, templateID)
	if err != nil {
		return template.DeliverySummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	summary := template.DeliverySummary{FailureReasons: []delivery.FailureGroup{}}
	failureCounts := map[string]int{}
	for rows.Next() {
		var status, failureReason string
		var retryCount int
		if err := rows.Scan(&status, &retryCount, &failureReason); err != nil {
			return template.DeliverySummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		summary.TotalAttempts++
		if summary.RetryCountMin == nil || retryCount < *summary.RetryCountMin {
			summary.RetryCountMin = intPtr(retryCount)
		}
		if summary.RetryCountMax == nil || retryCount > *summary.RetryCountMax {
			summary.RetryCountMax = intPtr(retryCount)
		}
		switch status {
		case delivery.StatusPending:
			summary.StatusCounts.Pending++
		case delivery.StatusRetrying:
			summary.StatusCounts.Retrying++
		case delivery.StatusSucceeded:
			summary.StatusCounts.Succeeded++
		case delivery.StatusFailed:
			summary.StatusCounts.Failed++
		}
		if (status == delivery.StatusFailed || status == delivery.StatusRetrying) && failureReason != "" {
			failureCounts[failureReason]++
		}
	}
	if err := rows.Err(); err != nil {
		return template.DeliverySummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	reasons := make([]delivery.FailureGroup, 0, len(failureCounts))
	for reason, count := range failureCounts {
		reasons = append(reasons, delivery.FailureGroup{FailureReason: reason, AttemptCount: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].AttemptCount != reasons[j].AttemptCount {
			return reasons[i].AttemptCount > reasons[j].AttemptCount
		}
		return reasons[i].FailureReason < reasons[j].FailureReason
	})
	summary.FailureReasons = reasons
	return summary, nil
}

// isConstraintError reports whether err carries a SQLite constraint result
// code, which the single-column primary key turns into a duplicate
// template_id rejection.
func isConstraintError(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code()&0xff == sqliteConstraint
	}
	return false
}
