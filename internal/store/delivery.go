package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// ErrStorageUnavailable marks a failed storage call. The HTTP layer maps it to
// the published STORAGE_UNAVAILABLE response instead of leaking driver errors.
var ErrStorageUnavailable = errors.New("storage is unavailable")

// storedTimeFormat is a fixed-width UTC layout. Fixed width makes the text
// column sort in chronological order and keeps range comparisons exact.
const storedTimeFormat = "2006-01-02T15:04:05.000000000Z"

// CreateDeliveryRecord inserts one delivery attempt. Every attempt gets its
// own row and its own id; retries never overwrite earlier failure reasons.
func (s *Store) CreateDeliveryRecord(ctx context.Context, record delivery.Record) (delivery.Record, error) {
	record.ID = uuid.NewString()
	storedAt := record.OccurredAt.UTC().Format(storedTimeFormat)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO delivery_records
		    (id, template_id, channel, occurred_at, status, retry_count, failure_reason)
		  VALUES (?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.TemplateID, record.Channel, storedAt,
		record.Status, record.RetryCount, record.FailureReason,
	)
	if err != nil {
		return delivery.Record{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return record, nil
}

// CreateDeliveryRecords inserts a batch of delivery attempts atomically. Each
// record gets its own row and its own id, and request order is preserved in
// the returned slice. The batch either commits completely or rolls back, so a
// failed batch never leaves partial records behind.
func (s *Store) CreateDeliveryRecords(ctx context.Context, records []delivery.Record) ([]delivery.Record, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	commitErr := func(err error) ([]delivery.Record, error) {
		_ = tx.Rollback()
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO delivery_records
		    (id, template_id, channel, occurred_at, status, retry_count, failure_reason)
		  VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return commitErr(err)
	}
	defer stmt.Close()

	saved := make([]delivery.Record, len(records))
	for i, record := range records {
		record.ID = uuid.NewString()
		storedAt := record.OccurredAt.UTC().Format(storedTimeFormat)
		if _, err := stmt.ExecContext(ctx,
			record.ID, record.TemplateID, record.Channel, storedAt,
			record.Status, record.RetryCount, record.FailureReason,
		); err != nil {
			return commitErr(err)
		}
		saved[i] = record
	}
	if err := tx.Commit(); err != nil {
		return commitErr(err)
	}
	return saved, nil
}

// GetDeliveryRecord loads one record by id. The boolean is false when no row
// exists; that is not a storage failure.
func (s *Store) GetDeliveryRecord(ctx context.Context, id string) (delivery.Record, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason
		   FROM delivery_records WHERE id = ?`, id)

	record, err := scanDeliveryRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Record{}, false, nil
	}
	if err != nil {
		return delivery.Record{}, false, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return record, true, nil
}

// ListDeliveryRecords returns registered records for one channel whose
// occurrence time falls in the half-open [start, end) range, ordered by
// occurrence time ascending.
func (s *Store) ListDeliveryRecords(ctx context.Context, query delivery.Query) ([]delivery.Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason
		   FROM delivery_records
		  WHERE channel = ? AND occurred_at >= ? AND occurred_at < ?
		  ORDER BY occurred_at ASC, id ASC`,
		query.Channel,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	var records []delivery.Record
	for rows.Next() {
		record, err := scanDeliveryRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return records, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDeliveryRecord(scanner rowScanner) (delivery.Record, error) {
	var record delivery.Record
	var occurredAt string
	if err := scanner.Scan(
		&record.ID, &record.TemplateID, &record.Channel, &occurredAt,
		&record.Status, &record.RetryCount, &record.FailureReason,
	); err != nil {
		return delivery.Record{}, err
	}
	parsedAt, err := time.Parse(storedTimeFormat, occurredAt)
	if err != nil {
		return delivery.Record{}, err
	}
	record.OccurredAt = parsedAt.UTC()
	return record, nil
}
