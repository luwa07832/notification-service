package store

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedSearchRecords(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "HTTP 503 provider busy"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(2), FailureReason: "http timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:15Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(3), FailureReason: "HTTP 503 provider dead"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "bounce"},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
}

func mustSearch(t *testing.T, mutate func(*delivery.SearchInput)) delivery.SearchQuery {
	t.Helper()
	in := delivery.SearchInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
	mutate(&in)
	query, err := delivery.NewSearch(in)
	if err != nil {
		t.Fatalf("new search: %v", err)
	}
	return query
}

func TestSearchDeliveryRecordsFiltersAndTotals(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	query := mustSearch(t, func(in *delivery.SearchInput) {
		templateID := "tpl-1"
		in.TemplateID = &templateID
	})
	result, err := st.SearchDeliveryRecords(context.Background(), query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 4 {
		t.Fatalf("total = %d, want 4", result.Total)
	}
	if len(result.Records) != 4 {
		t.Fatalf("page len = %d, want 4", len(result.Records))
	}
	wantOrder := []string{
		"2026-10-01T10:00:00Z",
		"2026-10-01T10:00:05Z",
		"2026-10-01T10:00:10Z",
		"2026-10-01T10:00:15Z",
	}
	for i, want := range wantOrder {
		if got := result.Records[i].OccurredAt.Format("2006-01-02T15:04:05Z"); got != want {
			t.Fatalf("record %d occurred at = %s, want %s", i, got, want)
		}
	}
}

func TestSearchDeliveryRecordsCombinesEveryCondition(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	query := mustSearch(t, func(in *delivery.SearchInput) {
		templateID := "tpl-1"
		status := delivery.StatusRetrying
		retryMin := "2"
		retryMax := "5"
		reason := "http"
		in.TemplateID = &templateID
		in.Status = &status
		in.RetryMin = &retryMin
		in.RetryMax = &retryMax
		in.FailureReasonContain = &reason
	})
	result, err := st.SearchDeliveryRecords(context.Background(), query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("total = %d, want 1", result.Total)
	}
	if len(result.Records) != 1 || result.Records[0].FailureReason != "http timeout" {
		t.Fatalf("unexpected records: %+v", result.Records)
	}
}

func TestSearchDeliveryRecordsFailureReasonIsCaseSensitive(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	query := mustSearch(t, func(in *delivery.SearchInput) {
		reason := "PROVIDER"
		in.FailureReasonContain = &reason
	})
	result, err := st.SearchDeliveryRecords(context.Background(), query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 0 || len(result.Records) != 0 {
		t.Fatalf("case-sensitive match leaked records: %+v", result)
	}
}

func TestSearchDeliveryRecordsRetryBounds(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	query := mustSearch(t, func(in *delivery.SearchInput) {
		retryMin := "1"
		retryMax := "2"
		in.RetryMin = &retryMin
		in.RetryMax = &retryMax
	})
	result, err := st.SearchDeliveryRecords(context.Background(), query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("total = %d, want 2", result.Total)
	}
	for _, record := range result.Records {
		if record.RetryCount < 1 || record.RetryCount > 2 {
			t.Fatalf("retry count %d outside [1,2]", record.RetryCount)
		}
	}
}

func TestSearchDeliveryRecordsPaginatesWithAccurateTotal(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	fetch := func(page int) SearchResult {
		query := mustSearch(t, func(in *delivery.SearchInput) {
			pageSize := "2"
			pageValue := strconv.Itoa(page)
			in.Page = &pageValue
			in.PageSize = &pageSize
		})
		result, err := st.SearchDeliveryRecords(context.Background(), query)
		if err != nil {
			t.Fatalf("search page %d: %v", page, err)
		}
		return result
	}

	first := fetch(1)
	if first.Total != 5 || len(first.Records) != 2 {
		t.Fatalf("first page total=%d len=%d", first.Total, len(first.Records))
	}
	second := fetch(2)
	if second.Total != 5 || len(second.Records) != 2 {
		t.Fatalf("second page total=%d len=%d", second.Total, len(second.Records))
	}
	third := fetch(3)
	if third.Total != 5 || len(third.Records) != 1 {
		t.Fatalf("third page total=%d len=%d", third.Total, len(third.Records))
	}
	if first.Records[0].ID == second.Records[0].ID {
		t.Fatal("pages returned overlapping records")
	}
	if got := third.Records[0].OccurredAt.Format("15:04:05"); got != "10:00:15" {
		t.Fatalf("last record time = %s, want 10:00:15", got)
	}

	pastEnd := fetch(4)
	if pastEnd.Total != 5 || len(pastEnd.Records) != 0 {
		t.Fatalf("past-end page total=%d len=%d, want 5 and 0", pastEnd.Total, len(pastEnd.Records))
	}
}

func TestSearchDeliveryRecordsEmptyMatch(t *testing.T) {
	st := openTestStore(t)
	seedSearchRecords(t, st)

	query := mustSearch(t, func(in *delivery.SearchInput) {
		templateID := "tpl-missing"
		in.TemplateID = &templateID
	})
	result, err := st.SearchDeliveryRecords(context.Background(), query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("total = %d, want 0", result.Total)
	}
	if result.Records == nil || len(result.Records) != 0 {
		t.Fatalf("records = %#v, want non-nil empty slice", result.Records)
	}
}

func TestSearchDeliveryRecordsReportsStorageUnavailableWhenClosed(t *testing.T) {
	st := openTestStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	query, err := delivery.NewSearch(delivery.SearchInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	})
	if err != nil {
		t.Fatalf("new search: %v", err)
	}
	if _, err := st.SearchDeliveryRecords(context.Background(), query); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("err = %v, want ErrStorageUnavailable", err)
	}
}

func TestSearchDeliveryRecordsOrdersSameTimeByID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		record := mustRecord(t, delivery.RecordInput{
			TemplateID:    "same",
			Channel:       "sms",
			OccurredAt:    "2026-10-01T10:00:00Z",
			Status:        delivery.StatusFailed,
			RetryCount:    ptrInt(i),
			FailureReason: "same moment",
		})
		if _, err := st.CreateDeliveryRecord(ctx, record); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	query, err := delivery.NewSearch(delivery.SearchInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	})
	if err != nil {
		t.Fatalf("new search: %v", err)
	}
	result, err := st.SearchDeliveryRecords(ctx, query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 5 || len(result.Records) != 5 {
		t.Fatalf("total=%d len=%d, want 5/5", result.Total, len(result.Records))
	}
	for i := 1; i < len(result.Records); i++ {
		if result.Records[i-1].ID >= result.Records[i].ID {
			t.Fatalf("same-timestamp records not ordered by id: %s before %s",
				result.Records[i-1].ID, result.Records[i].ID)
		}
	}
}
