package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func reason(v string) *string { return &v }

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func saveRecord(t *testing.T, st *Store, channel, occurredAt, status string, retries int, failure *string) delivery.Record {
	t.Helper()
	when, err := delivery.ParseTime(occurredAt)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	saved, err := st.SaveDeliveryRecord(context.Background(), delivery.Record{
		TemplateID:    "tpl-welcome",
		Channel:       channel,
		OccurredAt:    occurredAt,
		Status:        status,
		RetryCount:    retries,
		FailureReason: failure,
	}, when)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	return saved
}

func TestSaveAssignsUniqueIDAndRoundTrips(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	first := saveRecord(t, st, "email", "2026-10-01T08:00:00Z", delivery.StatusFailed, 0, reason("bounced"))
	second := saveRecord(t, st, "email", "2026-10-01T08:00:00Z", delivery.StatusSucceeded, 1, nil)

	if first.ID == "" || first.ID == second.ID {
		t.Fatalf("ids must be unique and set: %q %q", first.ID, second.ID)
	}
	loaded, err := st.GetDeliveryRecord(ctx, first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.TemplateID != "tpl-welcome" || loaded.Channel != "email" || loaded.Status != "failed" ||
		loaded.RetryCount != 0 || loaded.FailureReason == nil || *loaded.FailureReason != "bounced" {
		t.Fatalf("record round trip mismatch: %+v", loaded)
	}
}

func TestGetUnknownRecordReturnsNotFound(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.GetDeliveryRecord(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListUsesHalfOpenRangeAndAscendingOrder(t *testing.T) {
	st := newTestStore(t)

	saveRecord(t, st, "sms", "2026-10-01T07:59:59Z", delivery.StatusFailed, 0, reason("before window"))
	saveRecord(t, st, "sms", "2026-10-01T08:00:00Z", delivery.StatusPending, 0, nil)
	saveRecord(t, st, "sms", "2026-10-01T08:00:05Z", delivery.StatusRetrying, 1, reason("timeout"))
	saveRecord(t, st, "sms", "2026-10-01T08:00:10Z", delivery.StatusSucceeded, 2, nil)
	saveRecord(t, st, "sms", "2026-10-01T09:00:00Z", delivery.StatusFailed, 3, reason("end boundary"))
	saveRecord(t, st, "email", "2026-10-01T08:00:05Z", delivery.StatusFailed, 0, reason("other channel"))

	start := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	got, err := st.ListDeliveryRecords(context.Background(), "sms", start, end)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	wantTimes := []string{
		"2026-10-01T08:00:00Z",
		"2026-10-01T08:00:05Z",
		"2026-10-01T08:00:10Z",
	}
	if len(got) != len(wantTimes) {
		t.Fatalf("got %d records %+v, want %d", len(got), got, len(wantTimes))
	}
	for i, rec := range got {
		if rec.OccurredAt != wantTimes[i] {
			t.Fatalf("record %d time = %q, want %q (order or range wrong)", i, rec.OccurredAt, wantTimes[i])
		}
	}
	if got[1].Status != "retrying" || got[1].RetryCount != 1 || *got[1].FailureReason != "timeout" {
		t.Fatalf("retry stage not preserved: %+v", got[1])
	}
}

func TestListEmptyRangeReturnsEmptySlice(t *testing.T) {
	st := newTestStore(t)
	start := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	got, err := st.ListDeliveryRecords(context.Background(), "push", start, end)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d records, want none", len(got))
	}
}

func TestRepeatedAttemptsNeverOverwriteFailureReason(t *testing.T) {
	st := newTestStore(t)
	first := saveRecord(t, st, "push", "2026-10-01T08:00:00Z", delivery.StatusFailed, 0, reason("first failure"))
	saveRecord(t, st, "push", "2026-10-01T08:01:00Z", delivery.StatusRetrying, 1, reason("second failure"))
	saveRecord(t, st, "push", "2026-10-01T08:02:00Z", delivery.StatusSucceeded, 2, nil)

	loaded, err := st.GetDeliveryRecord(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.Status != delivery.StatusFailed || *loaded.FailureReason != "first failure" || loaded.RetryCount != 0 {
		t.Fatalf("earlier attempt was overwritten: %+v", loaded)
	}
}
