package store

import (
	"context"
	"fmt"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// NotificationDeliveryHistory returns every registered attempt carrying the
// given notification identifier, ordered by occurrence time ascending with
// ties broken by id ascending. The query only reads existing rows: it never
// fabricates or modifies delivery facts.
func (s *Store) NotificationDeliveryHistory(ctx context.Context, notificationID string) ([]delivery.Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, template_id, channel, occurred_at, status, retry_count, failure_reason, notification_id
		   FROM delivery_records
		  WHERE notification_id = ?
		  ORDER BY occurred_at ASC, id ASC`,
		notificationID,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	records := []delivery.Record{}
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

// HasDeliveryRecords reports whether any delivery attempt has ever been
// registered, tagged or not.
func (s *Store) HasDeliveryRecords(ctx context.Context) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM delivery_records)`,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return exists != 0, nil
}
