package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func ptrStr(v string) *string { return &v }

func TestNotificationIDRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	tagged := mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
		Status: delivery.StatusFailed, RetryCount: ptrInt(1),
		FailureReason: "boom", NotificationID: ptrStr("ntf-1"),
	})
	saved, err := st.CreateDeliveryRecord(ctx, tagged)
	if err != nil {
		t.Fatalf("create tagged: %v", err)
	}
	if saved.NotificationID == nil || *saved.NotificationID != "ntf-1" {
		t.Fatalf("saved notification id = %v, want ntf-1", saved.NotificationID)
	}
	got, ok, err := st.GetDeliveryRecord(ctx, saved.ID)
	if err != nil || !ok {
		t.Fatalf("get tagged: ok=%v err=%v", ok, err)
	}
	if got.NotificationID == nil || *got.NotificationID != "ntf-1" {
		t.Fatalf("loaded notification id = %v, want ntf-1", got.NotificationID)
	}

	untagged := mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:01:00Z",
		Status: delivery.StatusSucceeded, RetryCount: ptrInt(0),
	})
	savedUntagged, err := st.CreateDeliveryRecord(ctx, untagged)
	if err != nil {
		t.Fatalf("create untagged: %v", err)
	}
	got, ok, err = st.GetDeliveryRecord(ctx, savedUntagged.ID)
	if err != nil || !ok {
		t.Fatalf("get untagged: ok=%v err=%v", ok, err)
	}
	if got != savedUntagged {
		t.Fatalf("untagged round trip mismatch:\n got=%#v\nwant=%#v", got, savedUntagged)
	}
}

func TestNotificationDeliveryHistoryFiltersAndOrders(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	seed := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "second",
			NotificationID: ptrStr("ntf-1")},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "first",
			NotificationID: ptrStr("ntf-1")},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "other notification",
			NotificationID: ptrStr("ntf-2")},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
	}
	for i, in := range seed {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	records, err := st.NotificationDeliveryHistory(ctx, "ntf-1")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len = %d, want 2", len(records))
	}
	if records[0].FailureReason != "first" || records[1].FailureReason != "second" {
		t.Fatalf("order = %q,%q, want first,second",
			records[0].FailureReason, records[1].FailureReason)
	}
	for _, record := range records {
		if record.NotificationID == nil || *record.NotificationID != "ntf-1" {
			t.Fatalf("record %s notification id = %v, want ntf-1", record.ID, record.NotificationID)
		}
	}

	empty, err := st.NotificationDeliveryHistory(ctx, "ntf-missing")
	if err != nil {
		t.Fatalf("history missing: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("len = %d, want 0", len(empty))
	}
}

func TestNotificationDeliveryHistoryTiesBreakOnID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ids := []string{}
	for i := 0; i < 3; i++ {
		saved, err := st.CreateDeliveryRecord(ctx, mustRecord(t, delivery.RecordInput{
			TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(i),
			NotificationID: ptrStr("ntf-tie"),
		}))
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ids = append(ids, saved.ID)
	}

	records, err := st.NotificationDeliveryHistory(ctx, "ntf-tie")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("len = %d, want 3", len(records))
	}
	sort.Strings(ids)
	for i, record := range records {
		if record.ID != ids[i] {
			t.Fatalf("record %d id = %s, want %s (id ascending)", i, record.ID, ids[i])
		}
	}
}

func TestHasDeliveryRecords(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	has, err := st.HasDeliveryRecords(ctx)
	if err != nil {
		t.Fatalf("fresh has: %v", err)
	}
	if has {
		t.Fatal("fresh store reports records")
	}

	if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
		Status: delivery.StatusSucceeded, RetryCount: ptrInt(0),
	})); err != nil {
		t.Fatalf("create: %v", err)
	}
	has, err = st.HasDeliveryRecords(ctx)
	if err != nil {
		t.Fatalf("has after create: %v", err)
	}
	if !has {
		t.Fatal("store with one record reports none")
	}
}

func TestOpenAddsNotificationIDToLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE delivery_records (
		id             TEXT PRIMARY KEY,
		template_id    TEXT NOT NULL,
		channel        TEXT NOT NULL,
		occurred_at    TEXT NOT NULL,
		status         TEXT NOT NULL,
		retry_count    INTEGER NOT NULL CHECK (retry_count >= 0),
		failure_reason TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open with migration: %v", err)
	}
	defer st.Close()

	saved, err := st.CreateDeliveryRecord(context.Background(), mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
		Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "boom",
		NotificationID: ptrStr("ntf-legacy"),
	}))
	if err != nil {
		t.Fatalf("create after migration: %v", err)
	}
	records, err := st.NotificationDeliveryHistory(context.Background(), "ntf-legacy")
	if err != nil {
		t.Fatalf("history after migration: %v", err)
	}
	if len(records) != 1 || records[0].ID != saved.ID {
		t.Fatalf("history = %+v, want exactly the migrated record", records)
	}
}

func TestNotificationHistoryReportsStorageUnavailableWhenClosed(t *testing.T) {
	st := openTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	if _, err := st.NotificationDeliveryHistory(ctx, "ntf-1"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("history err = %v, want ErrStorageUnavailable", err)
	}
	if _, err := st.HasDeliveryRecords(ctx); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("has err = %v, want ErrStorageUnavailable", err)
	}
}
