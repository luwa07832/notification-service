package store

import (
	"context"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func batchRecords(t *testing.T, n int) []delivery.Record {
	t.Helper()
	records := make([]delivery.Record, n)
	for i := range records {
		record, err := delivery.NewRecord(delivery.RecordInput{
			TemplateID:    "tpl-1",
			Channel:       "sms",
			OccurredAt:    "2026-10-01T10:00:00Z",
			Status:        delivery.StatusFailed,
			RetryCount:    ptrInt(i),
			FailureReason: "provider timeout",
		})
		if err != nil {
			t.Fatalf("new record %d: %v", i, err)
		}
		records[i] = record
	}
	return records
}

func TestCreateDeliveryRecordsRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	saved, err := st.CreateDeliveryRecords(ctx, batchRecords(t, 3))
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if len(saved) != 3 {
		t.Fatalf("len = %d, want 3", len(saved))
	}

	seen := map[string]struct{}{}
	for i, record := range saved {
		if record.ID == "" {
			t.Fatalf("record %d has no id", i)
		}
		if _, dup := seen[record.ID]; dup {
			t.Fatalf("duplicate id %s", record.ID)
		}
		seen[record.ID] = struct{}{}
		if record.RetryCount != i {
			t.Fatalf("record %d retry_count = %d, submitted order not kept", i, record.RetryCount)
		}
		got, ok, err := st.GetDeliveryRecord(ctx, record.ID)
		if err != nil || !ok {
			t.Fatalf("get %d: ok=%v err=%v", i, ok, err)
		}
		if got != record {
			t.Fatalf("round trip %d:\n got=%#v\nwant=%#v", i, got, record)
		}
	}
}

func TestCreateDeliveryRecordsRollsBackMidBatchFailure(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	// Fail the second insert of the next transaction; earlier inserts in the
	// same batch must roll back too.
	_, err := st.db.Exec(`
CREATE TRIGGER fail_second_insert
BEFORE INSERT ON delivery_records
WHEN (SELECT COUNT(*) FROM delivery_records) = 1
BEGIN
	SELECT RAISE(ABORT, 'injected mid-batch failure');
END;`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err = st.CreateDeliveryRecords(ctx, batchRecords(t, 3))
	if !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("create err = %v, want ErrStorageUnavailable", err)
	}

	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_records`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("partial batch left %d records, want 0", count)
	}

	if _, err := st.db.Exec(`DROP TRIGGER fail_second_insert`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	// A fresh batch must succeed and leave no trace of the failed round.
	saved, err := st.CreateDeliveryRecords(ctx, batchRecords(t, 2))
	if err != nil {
		t.Fatalf("create after rollback: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("len = %d, want 2", len(saved))
	}
}

func TestCreateDeliveryRecordsReportsStorageUnavailableWhenClosed(t *testing.T) {
	st := openTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := st.CreateDeliveryRecords(context.Background(), batchRecords(t, 2)); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("err = %v, want ErrStorageUnavailable", err)
	}
}
