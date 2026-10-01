package store

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func mustSearchQuery(t *testing.T, in delivery.SearchInput) delivery.SearchQuery {
	t.Helper()
	query, err := delivery.NewSearchQuery(in)
	if err != nil {
		t.Fatalf("new search query: %v", err)
	}
	return query
}

func seedSearchRecords(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "provider timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "Provider Timeout"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:15Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(4), FailureReason: "provider timeout"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:20Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "provider timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:01:00Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(3)},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
}

func searchWindow() delivery.SearchInput {
	return delivery.SearchInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestSearchDeliveryRecordsFiltersEveryCondition(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)
	ctx := context.Background()

	in := searchWindow()
	in.TemplateID, in.TemplateIDSet = " tpl-1 ", true
	in.Status, in.StatusSet = delivery.StatusFailed, true
	in.RetryMin, in.RetryMinSet = "2", true
	in.RetryMax, in.RetryMaxSet = "2", true
	in.FailureReasonContains, in.FailureReasonContainsSet = "Provider Timeout", true

	records, total, err := st.SearchDeliveryRecords(ctx, mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("total=%d len=%d, want 1 and 1", total, len(records))
	}
	got := records[0]
	if got.TemplateID != "tpl-1" || got.Channel != "sms" || got.Status != "failed" ||
		got.RetryCount != 2 || got.FailureReason != "Provider Timeout" {
		t.Fatalf("unexpected record: %#v", got)
	}
}

func TestSearchDeliveryRecordsFailureReasonIsCaseSensitive(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	in := searchWindow()
	in.FailureReasonContains, in.FailureReasonContainsSet = "Provider Timeout", true
	records, total, err := st.SearchDeliveryRecords(context.Background(), mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(records) != 1 || records[0].FailureReason != "Provider Timeout" {
		t.Fatalf("total=%d records=%+v, want the single capitalized reason", total, records)
	}
}

func TestSearchDeliveryRecordsHonorsHalfOpenRangeAndChannel(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	in := searchWindow()
	in.Start = "2026-10-01T10:00:05Z"
	in.End = "2026-10-01T10:01:00Z"
	records, total, err := st.SearchDeliveryRecords(context.Background(), mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3 (end excluded, email excluded)", total)
	}
	for _, record := range records {
		if record.Channel != "sms" {
			t.Fatalf("wrong channel leaked in: %#v", record)
		}
	}
}

func TestSearchDeliveryRecordsPaginatesWithAccurateTotal(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)
	ctx := context.Background()

	in := searchWindow()
	in.PageSize, in.PageSizeSet = "2", true

	in.Page, in.PageSet = "1", true
	page1, total, err := st.SearchDeliveryRecords(ctx, mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if total != 5 || len(page1) != 2 {
		t.Fatalf("page 1: total=%d len=%d, want 5 and 2", total, len(page1))
	}

	in.Page = "3"
	page3, total, err := st.SearchDeliveryRecords(ctx, mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if total != 5 || len(page3) != 1 {
		t.Fatalf("page 3: total=%d len=%d, want 5 and 1", total, len(page3))
	}
	if page3[0].Status != delivery.StatusSucceeded {
		t.Fatalf("page 3 record = %#v, want the succeeded one", page3[0])
	}

	in.Page = "4"
	page4, total, err := st.SearchDeliveryRecords(ctx, mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("page 4: %v", err)
	}
	if total != 5 || len(page4) != 0 {
		t.Fatalf("page 4: total=%d len=%d, want 5 and 0", total, len(page4))
	}
}

func TestSearchDeliveryRecordsOrdersByTimeThenID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	var wantIDs []string
	for i := 0; i < 3; i++ {
		record := mustRecord(t, delivery.RecordInput{
			TemplateID: "tpl-tie", Channel: "push", OccurredAt: "2026-10-01T12:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(i), FailureReason: "boom",
		})
		saved, err := st.CreateDeliveryRecord(ctx, record)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		wantIDs = append(wantIDs, saved.ID)
	}
	sort.Strings(wantIDs)

	in := delivery.SearchInput{
		Channel: "push",
		Start:   "2026-10-01T12:00:00Z",
		End:     "2026-10-01T12:00:01Z",
	}
	records, total, err := st.SearchDeliveryRecords(ctx, mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 3 || len(records) != 3 {
		t.Fatalf("total=%d len=%d, want 3 and 3", total, len(records))
	}
	for i, record := range records {
		if record.ID != wantIDs[i] {
			t.Fatalf("record %d id = %s, want %s (id tie-break)", i, record.ID, wantIDs[i])
		}
	}
}

func TestSearchDeliveryRecordsEmptyResultKeepsTotal(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	in := searchWindow()
	in.TemplateID, in.TemplateIDSet = "tpl-absent", true
	records, total, err := st.SearchDeliveryRecords(context.Background(), mustSearchQuery(t, in))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 0 || len(records) != 0 {
		t.Fatalf("total=%d len=%d, want 0 and 0", total, len(records))
	}
}

func TestSearchDeliveryRecordsReportsStorageUnavailableWhenClosed(t *testing.T) {
	st := openTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, _, err := st.SearchDeliveryRecords(context.Background(), mustSearchQuery(t, searchWindow()))
	if !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("err = %v, want ErrStorageUnavailable", err)
	}
}
