package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// ErrNotFound is returned when no delivery record matches the requested identifier.
var ErrNotFound = errors.New("delivery record not found")

// SaveDeliveryRecord stores one delivery attempt. Every attempt is inserted independently:
// repeated notifications produce separate rows ordered by their real occurrence time, and a
// later attempt never overwrites an earlier failure reason.
func (s *Store) SaveDeliveryRecord(ctx context.Context, rec delivery.Record, occurredAt time.Time) (delivery.Record, error) {
	id, err := newRecordID()
	if err != nil {
		return delivery.Record{}, fmt.Errorf("generate record id: %w", err)
	}
	rec.ID = id

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO delivery_records
			(id, template_id, channel, occurred_at, occurred_at_key, status, retry_count, failure_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.TemplateID, rec.Channel, rec.OccurredAt,
		delivery.CanonicalTime(occurredAt), rec.Status, rec.RetryCount, rec.FailureReason)
	if err != nil {
		return delivery.Record{}, fmt.Errorf("insert delivery record: %w", err)
	}
	return rec, nil
}

// GetDeliveryRecord loads a single attempt by its unique identifier.
func (s *Store) GetDeliveryRecord(ctx context.Context, id string) (delivery.Record, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason
		 FROM delivery_records WHERE id = ?`, id)

	rec, err := scanDeliveryRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Record{}, ErrNotFound
	}
	if err != nil {
		return delivery.Record{}, err
	}
	return rec, nil
}

// ListDeliveryRecords returns attempts for channel whose occurrence time lies in the
// half-open interval [start, end), ordered by occurrence time ascending. Rows sharing an
// instant keep insertion order so a notification and its retries read in attempt order.
func (s *Store) ListDeliveryRecords(ctx context.Context, channel string, start, end time.Time) ([]delivery.Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason
		 FROM delivery_records
		 WHERE channel = ? AND occurred_at_key >= ? AND occurred_at_key < ?
		 ORDER BY occurred_at_key ASC, rowid ASC`,
		channel, delivery.CanonicalTime(start), delivery.CanonicalTime(end))
	if err != nil {
		return nil, fmt.Errorf("query delivery records: %w", err)
	}
	defer rows.Close()

	records := make([]delivery.Record, 0)
	for rows.Next() {
		rec, err := scanDeliveryRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan delivery records: %w", err)
	}
	return records, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDeliveryRecord(scanner rowScanner) (delivery.Record, error) {
	var rec delivery.Record
	var reason sql.NullString
	if err := scanner.Scan(
		&rec.ID, &rec.TemplateID, &rec.Channel, &rec.OccurredAt,
		&rec.Status, &rec.RetryCount, &reason,
	); err != nil {
		return delivery.Record{}, err
	}
	if reason.Valid {
		rec.FailureReason = &reason.String
	}
	return rec, nil
}

// newRecordID returns a version-4 UUID string used as the unique record identifier.
func newRecordID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
