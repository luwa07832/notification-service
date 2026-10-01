package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func mustRecord(t *testing.T, in delivery.RecordInput) delivery.Record {
	t.Helper()
	record, err := delivery.NewRecord(in)
	if err != nil {
		t.Fatalf("new record: %v", err)
	}
	return record
}

func ptrInt(v int) *int { return &v }

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCreateAndGetDeliveryRecordRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	record := mustRecord(t, delivery.RecordInput{
		TemplateID:    "tpl-1",
		Channel:       "sms",
		OccurredAt:    "2026-10-01T10:00:05.5Z",
		Status:        delivery.StatusFailed,
		RetryCount:    ptrInt(2),
		FailureReason: "provider timeout",
	})

	saved, err := st.CreateDeliveryRecord(ctx, record)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("saved record has no id")
	}

	got, ok, err := st.GetDeliveryRecord(ctx, saved.ID)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got != saved {
		t.Fatalf("round trip mismatch:\n got=%#v\nwant=%#v", got, saved)
	}
	if got.TemplateID != "tpl-1" || got.Channel != "sms" || got.Status != "failed" ||
		got.RetryCount != 2 || got.FailureReason != "provider timeout" {
		t.Fatalf("unexpected stored content: %#v", got)
	}
}

func TestGetDeliveryRecordMissing(t *testing.T) {
	st := openTestStore(t)
	_, ok, err := st.GetDeliveryRecord(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("missing record reported as present")
	}
}

func TestListDeliveryRecordsHalfOpenRangeAndOrder(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	times := []string{
		"2026-10-01T09:59:59Z",
		"2026-10-01T10:00:00Z",
		"2026-10-01T10:30:00Z",
		"2026-10-01T11:00:00Z",
		"2026-10-01T11:00:01Z",
	}
	for i, at := range times {
		record := mustRecord(t, delivery.RecordInput{
			TemplateID: "tpl-1", Channel: "email", OccurredAt: at,
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(i),
		})
		if _, err := st.CreateDeliveryRecord(ctx, record); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	// A record for another channel must not appear.
	other := mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:30:00Z",
		Status: delivery.StatusSucceeded, RetryCount: ptrInt(0),
	})
	if _, err := st.CreateDeliveryRecord(ctx, other); err != nil {
		t.Fatalf("create other: %v", err)
	}

	query, err := delivery.NewQuery("email", "2026-10-01T10:00:00Z", "2026-10-01T11:00:00Z")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	records, err := st.ListDeliveryRecords(ctx, query)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len = %d, want 2", len(records))
	}
	if got, want := records[0].OccurredAt.Format(time.RFC3339), "2026-10-01T10:00:00Z"; got != want {
		t.Fatalf("first occurred at = %s, want %s", got, want)
	}
	if got, want := records[1].OccurredAt.Format(time.RFC3339), "2026-10-01T10:30:00Z"; got != want {
		t.Fatalf("second occurred at = %s, want %s", got, want)
	}
}

func TestListDeliveryRecordsKeepsEveryRetryAttempt(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-x", Channel: "push", OccurredAt: "2026-10-01T12:00:00Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-x", Channel: "push", OccurredAt: "2026-10-01T12:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "first failure"},
		{TemplateID: "tpl-x", Channel: "push", OccurredAt: "2026-10-01T12:00:10Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "second failure"},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	query, _ := delivery.NewQuery("push", "2026-10-01T12:00:00Z", "2026-10-01T12:01:00Z")
	records, err := st.ListDeliveryRecords(ctx, query)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("len = %d, want 3", len(records))
	}
	if records[0].RetryCount != 0 || records[1].RetryCount != 1 || records[2].RetryCount != 2 {
		t.Fatalf("retry counts = %d,%d,%d, want 0,1,2",
			records[0].RetryCount, records[1].RetryCount, records[2].RetryCount)
	}
	if records[1].FailureReason != "first failure" || records[2].FailureReason != "second failure" {
		t.Fatalf("failure reasons overwritten: %q, %q",
			records[1].FailureReason, records[2].FailureReason)
	}
	if records[0].FailureReason != "" {
		t.Fatalf("pending reason = %q, want empty", records[0].FailureReason)
	}
}

func TestListDeliveryRecordsEmptyRange(t *testing.T) {
	st := openTestStore(t)
	query, _ := delivery.NewQuery("sms", "2026-10-01T10:00:00Z", "2026-10-01T11:00:00Z")
	records, err := st.ListDeliveryRecords(context.Background(), query)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("len = %d, want 0", len(records))
	}
}

func TestDeliveryMethodsReportStorageUnavailableWhenClosed(t *testing.T) {
	st := openTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	if _, err := st.CreateDeliveryRecord(ctx, delivery.Record{Channel: "sms"}); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("create err = %v, want ErrStorageUnavailable", err)
	}
	if _, _, err := st.GetDeliveryRecord(ctx, "x"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("get err = %v, want ErrStorageUnavailable", err)
	}
	query, _ := delivery.NewQuery("sms", "2026-10-01T10:00:00Z", "2026-10-01T11:00:00Z")
	if _, err := st.ListDeliveryRecords(ctx, query); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("list err = %v, want ErrStorageUnavailable", err)
	}
}
