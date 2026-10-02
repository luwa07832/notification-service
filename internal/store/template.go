package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/template"
)

// ErrTemplateAlreadyExists marks a rejected template registration because the
// template identifier is already taken. The HTTP layer maps it to the
// published TEMPLATE_ALREADY_EXISTS response.
var ErrTemplateAlreadyExists = errors.New("notification template already exists")

// CreateNotificationTemplate inserts one template. Registration is terminal:
// no update or delete entry exists, and a repeated template_id is rejected
// instead of overwriting the stored template.
func (s *Store) CreateNotificationTemplate(ctx context.Context, t template.Template) error {
	encodedChannels, err := json.Marshal(t.Channels)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_templates
		    (template_id, name, body, channels, enabled)
		  VALUES (?, ?, ?, ?, ?)
		  ON CONFLICT(template_id) DO NOTHING`,
		t.TemplateID, t.Name, t.Body, string(encodedChannels), boolToInt(t.Enabled),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	if inserted == 0 {
		return ErrTemplateAlreadyExists
	}
	return nil
}

// GetNotificationTemplate loads one template by identifier. The boolean is
// false when no row exists; that is not a storage failure.
func (s *Store) GetNotificationTemplate(ctx context.Context, templateID string) (template.Template, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT template_id, name, body, channels, enabled
		   FROM notification_templates WHERE template_id = ?`, templateID)

	t, err := scanTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return template.Template{}, false, nil
	}
	if err != nil {
		return template.Template{}, false, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return t, true, nil
}

// ListNotificationTemplates returns registered templates matching the
// validated filters, ordered by template identifier ascending.
func (s *Store) ListNotificationTemplates(ctx context.Context, filter template.Filter) ([]template.Template, error) {
	clauses := []string{}
	args := []any{}
	if filter.Channel != nil {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM json_each(notification_templates.channels) WHERE value = ?)")
		args = append(args, *filter.Channel)
	}
	if filter.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, boolToInt(*filter.Enabled))
	}
	query := `SELECT template_id, name, body, channels, enabled
	            FROM notification_templates`
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY template_id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	templates := []template.Template{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return templates, nil
}

// TemplateDeliverySummary aggregates the registered delivery attempts of one
// template: the fixed status counts, retry-count bounds (nil without any
// attempt) and the verbatim failure reasons of failed or retrying attempts,
// ordered by count descending and then by reason text ascending. It only
// reads stored rows and never fabricates attempts.
func (s *Store) TemplateDeliverySummary(ctx context.Context, templateID string) (delivery.TemplateDeliverySummary, error) {
	summary := delivery.TemplateDeliverySummary{
		StatusCounts:   delivery.StatusCounts{},
		FailureReasons: []delivery.FailureGroup{},
	}

	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		        MIN(retry_count), MAX(retry_count)
		   FROM delivery_records WHERE template_id = ?`,
		delivery.StatusPending, delivery.StatusRetrying,
		delivery.StatusSucceeded, delivery.StatusFailed, templateID,
	).Scan(
		&summary.TotalAttempts,
		&summary.StatusCounts.Pending,
		&summary.StatusCounts.Retrying,
		&summary.StatusCounts.Succeeded,
		&summary.StatusCounts.Failed,
		&summary.RetryCountMin,
		&summary.RetryCountMax,
	)
	if err != nil {
		return delivery.TemplateDeliverySummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT failure_reason, COUNT(*)
		   FROM delivery_records
		  WHERE template_id = ? AND status IN (?, ?) AND failure_reason <> ''
		  GROUP BY failure_reason
		  ORDER BY COUNT(*) DESC, failure_reason ASC`,
		templateID, delivery.StatusFailed, delivery.StatusRetrying,
	)
	if err != nil {
		return delivery.TemplateDeliverySummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	for rows.Next() {
		var group delivery.FailureGroup
		if err := rows.Scan(&group.FailureReason, &group.AttemptCount); err != nil {
			return delivery.TemplateDeliverySummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		summary.FailureReasons = append(summary.FailureReasons, group)
	}
	if err := rows.Err(); err != nil {
		return delivery.TemplateDeliverySummary{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return summary, nil
}

type templateRowScanner interface {
	Scan(dest ...any) error
}

func scanTemplate(scanner templateRowScanner) (template.Template, error) {
	var (
		t               template.Template
		encodedChannels string
		enabled         int
	)
	if err := scanner.Scan(&t.TemplateID, &t.Name, &t.Body, &encodedChannels, &enabled); err != nil {
		return template.Template{}, err
	}
	if err := json.Unmarshal([]byte(encodedChannels), &t.Channels); err != nil {
		return template.Template{}, err
	}
	if t.Channels == nil {
		t.Channels = []string{}
	}
	t.Enabled = enabled != 0
	return t, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
