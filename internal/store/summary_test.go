package store

import (
	"context"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedSummaryRecords(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:15Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:20Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:25Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "bounce"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T11:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "at end boundary"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "Timeout"},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
}

func mustSummary(t *testing.T, mutate func(*delivery.SummaryInput)) delivery.SummaryQuery {
	t.Helper()
	in := delivery.SummaryInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
	mutate(&in)
	query, err := delivery.NewSummary(in)
	if err != nil {
		t.Fatalf("new summary: %v", err)
	}
	return query
}

func TestFailureSummaryGroupsVerbatimAndOrders(t *testing.T) {
	st := openTestStore(t)
	seedSummaryRecords(t, st)

	summary, err := st.FailureSummary(context.Background(), mustSummary(t, func(in *delivery.SummaryInput) {}))
	if err != nil {
		t.Fatalf("failure summary: %v", err)
	}
	if summary.TotalAttempts != 4 {
		t.Fatalf("total = %d, want 4", summary.TotalAttempts)
	}
	want := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "bounce", AttemptCount: 1},
		{FailureReason: "timeout", AttemptCount: 1},
	}
	if len(summary.Groups) != len(want) {
		t.Fatalf("groups = %+v, want %+v", summary.Groups, want)
	}
	for i := range want {
		if summary.Groups[i] != want[i] {
			t.Fatalf("group %d = %+v, want %+v", i, summary.Groups[i], want[i])
		}
	}
}

func TestFailureSummaryFiltersByTemplateID(t *testing.T) {
	st := openTestStore(t)
	seedSummaryRecords(t, st)

	templateID := "tpl-1"
	query := mustSummary(t, func(in *delivery.SummaryInput) { in.TemplateID = &templateID })
	summary, err := st.FailureSummary(context.Background(), query)
	if err != nil {
		t.Fatalf("failure summary: %v", err)
	}
	if summary.TotalAttempts != 3 {
		t.Fatalf("total = %d, want 3", summary.TotalAttempts)
	}
	want := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "timeout", AttemptCount: 1},
	}
	if len(summary.Groups) != len(want) || summary.Groups[0] != want[0] || summary.Groups[1] != want[1] {
		t.Fatalf("groups = %+v, want %+v", summary.Groups, want)
	}
}

func TestFailureSummaryEmptyResult(t *testing.T) {
	st := openTestStore(t)
	seedSummaryRecords(t, st)

	query := mustSummary(t, func(in *delivery.SummaryInput) { in.Channel = "push" })
	summary, err := st.FailureSummary(context.Background(), query)
	if err != nil {
		t.Fatalf("failure summary: %v", err)
	}
	if summary.TotalAttempts != 0 {
		t.Fatalf("total = %d, want 0", summary.TotalAttempts)
	}
	if len(summary.Groups) != 0 {
		t.Fatalf("groups = %+v, want empty", summary.Groups)
	}
}
