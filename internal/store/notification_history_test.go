package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func TestNotificationIDRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	tracked := mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
		Status: delivery.StatusSucceeded, RetryCount: ptrInt(0),
		NotificationID: strPtr("n-1"),
	})
	saved, err := st.CreateDeliveryRecord(ctx, tracked)
	if err != nil {
		t.Fatalf("create tracked: %v", err)
	}
	if saved.NotificationID != "n-1" {
		t.Fatalf("notification id = %q", saved.NotificationID)
	}

	untracked := mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:01Z",
		Status: delivery.StatusSucceeded, RetryCount: ptrInt(0),
	})
	if _, err := st.CreateDeliveryRecord(ctx, untracked); err != nil {
		t.Fatalf("create untracked: %v", err)
	}

	got, ok, err := st.GetDeliveryRecord(ctx, saved.ID)
	if err != nil || !ok {
		t.Fatalf("get tracked: ok=%v err=%v", ok, err)
	}
	if got.NotificationID != "n-1" {
		t.Fatalf("stored notification id = %q", got.NotificationID)
	}
}

func strPtr(v string) *string { return &v }

func TestListNotificationDeliveryHistoryOrderAndScope(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	type seed struct {
		notificationID string
		at             string
		status         string
		retry          int
		reason         string
	}
	seeds := []seed{
		{"n-1", "2026-10-01T10:00:10Z", delivery.StatusFailed, 2, "timeout"},
		{"n-2", "2026-10-01T10:00:00Z", delivery.StatusSucceeded, 0, ""},
		{"n-1", "2026-10-01T10:00:00Z", delivery.StatusPending, 0, ""},
		{"n-1", "2026-10-01T10:00:05Z", delivery.StatusRetrying, 1, "busy"},
	}
	// Insert in an order that proves ordering comes from the query, not insertion.
	for _, s := range seeds {
		in := delivery.RecordInput{
			TemplateID:     "tpl-1",
			Channel:        "sms",
			OccurredAt:     s.at,
			Status:         s.status,
			RetryCount:     ptrInt(s.retry),
			FailureReason:  s.reason,
			NotificationID: strPtr(s.notificationID),
		}
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %+v: %v", s, err)
		}
	}

	// Two rows share occurred_at across n-1/n-2; create a same-time pair for n-1
	// to verify the id tie break.
	sameA := mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
		Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "busy",
		NotificationID: strPtr("n-1"),
	})
	sameB := mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
		Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "busy",
		NotificationID: strPtr("n-1"),
	})
	createdA, err := st.CreateDeliveryRecord(ctx, sameA)
	if err != nil {
		t.Fatalf("create sameA: %v", err)
	}
	createdB, err := st.CreateDeliveryRecord(ctx, sameB)
	if err != nil {
		t.Fatalf("create sameB: %v", err)
	}

	records, found, err := st.ListNotificationDeliveryHistory(ctx, "n-1")
	if err != nil || !found {
		t.Fatalf("list: found=%v err=%v", found, err)
	}
	if len(records) != 5 {
		t.Fatalf("len = %d, want 5", len(records))
	}
	gotTimes := make([]string, len(records))
	for i, record := range records {
		gotTimes[i] = record.OccurredAt.Format(time.RFC3339)
	}
	wantTimes := []string{
		"2026-10-01T10:00:00Z",
		"2026-10-01T10:00:05Z",
		"2026-10-01T10:00:05Z",
		"2026-10-01T10:00:05Z",
		"2026-10-01T10:00:10Z",
	}
	for i := range wantTimes {
		if gotTimes[i] != wantTimes[i] {
			t.Fatalf("times[%d] = %s, want %s (full %v)", i, gotTimes[i], wantTimes[i], gotTimes)
		}
	}
	if records[1].ID > records[2].ID || records[2].ID > records[3].ID {
		t.Fatalf("same-time ids not ascending: %s %s %s", records[1].ID, records[2].ID, records[3].ID)
	}
	tieIDs := map[string]bool{createdA.ID: true, createdB.ID: true}
	seenTie := 0
	for _, record := range records[1:4] {
		if tieIDs[record.ID] {
			seenTie++
		}
	}
	if seenTie != 2 {
		t.Fatalf("inserted tie rows count = %d, want 2", seenTie)
	}
	for _, record := range records {
		if record.NotificationID != "n-1" {
			t.Fatalf("record %s belongs to %q", record.ID, record.NotificationID)
		}
	}
	// An instance with no registered attempts is reported as not found.
	if _, found, err := st.ListNotificationDeliveryHistory(ctx, "unknown"); err != nil || found {
		t.Fatalf("unknown: found=%v err=%v", found, err)
	}
}

func TestCreateDeliveryRecordsBatchPreservesNotificationIDs(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	records, err := delivery.NewRecords([]delivery.RecordInput{
		{
			TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "x",
			NotificationID: strPtr("n-1"),
		},
		{
			TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "y",
			NotificationID: strPtr("n-1"),
		},
	})
	if err != nil {
		t.Fatalf("new records: %v", err)
	}
	saved, err := st.CreateDeliveryRecords(ctx, records)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	for _, record := range saved {
		if record.NotificationID != "n-1" {
			t.Fatalf("notification id = %q", record.NotificationID)
		}
	}
}

func TestOpenMigratesDatabaseWithoutNotificationColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE delivery_records (
		id TEXT PRIMARY KEY, template_id TEXT NOT NULL, channel TEXT NOT NULL,
		occurred_at TEXT NOT NULL, status TEXT NOT NULL, retry_count INTEGER NOT NULL,
		failure_reason TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	_, err = db.Exec(`INSERT INTO delivery_records
		(id, template_id, channel, occurred_at, status, retry_count, failure_reason)
		VALUES ('id-1','tpl-1','sms','2026-10-01T10:00:00.000000000Z','succeeded',0,'')`)
	if err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open with migration: %v", err)
	}
	defer st.Close()

	record, ok, err := st.GetDeliveryRecord(context.Background(), "id-1")
	if err != nil || !ok {
		t.Fatalf("read legacy row: ok=%v err=%v", ok, err)
	}
	if record.NotificationID != "" {
		t.Fatalf("legacy notification id = %q, want empty", record.NotificationID)
	}
}
