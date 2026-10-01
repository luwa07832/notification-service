package store

import (
	"context"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedSummaryRecords(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	type attempt struct {
		templateID string
		channel    string
		occurredAt string
		status     string
		reason     string
	}
	attempts := []attempt{
		{"tpl-1", "sms", "2026-10-01T10:00:00Z", delivery.StatusFailed, "provider timeout"},
		{"tpl-1", "sms", "2026-10-01T10:01:00Z", delivery.StatusRetrying, "provider timeout"},
		{"tpl-1", "sms", "2026-10-01T10:02:00Z", delivery.StatusFailed, "provider timeout"},
		{"tpl-1", "sms", "2026-10-01T10:03:00Z", delivery.StatusFailed, "HTTP 503"},
		{"tpl-1", "sms", "2026-10-01T10:04:00Z", delivery.StatusRetrying, "HTTP 503"},
		{"tpl-1", "sms", "2026-10-01T10:05:00Z", delivery.StatusFailed, "bounce"},
		{"tpl-1", "sms", "2026-10-01T10:06:00Z", delivery.StatusFailed, "bounce"},
		{"tpl-1", "sms", "2026-10-01T10:07:00Z", delivery.StatusFailed, "http 503"},
		{"tpl-1", "sms", "2026-10-01T10:07:30Z", delivery.StatusFailed, " provider timeout"},
		{"tpl-1", "sms", "2026-10-01T10:08:00Z", delivery.StatusPending, ""},
		{"tpl-1", "sms", "2026-10-01T10:09:00Z", delivery.StatusSucceeded, ""},
		{"tpl-1", "sms", "2026-10-01T09:59:59Z", delivery.StatusFailed, "provider timeout"},
		{"tpl-1", "sms", "2026-10-01T11:00:00Z", delivery.StatusFailed, "provider timeout"},
		{"tpl-1", "email", "2026-10-01T10:10:00Z", delivery.StatusFailed, "provider timeout"},
		{"tpl-2", "sms", "2026-10-01T10:11:00Z", delivery.StatusFailed, "provider timeout"},
	}
	for i, a := range attempts {
		record := mustRecord(t, delivery.RecordInput{
			TemplateID: a.templateID, Channel: a.channel, OccurredAt: a.occurredAt,
			Status: a.status, RetryCount: ptrInt(0), FailureReason: a.reason,
		})
		if _, err := st.CreateDeliveryRecord(ctx, record); err != nil {
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

func TestSummarizeDeliveryFailuresGroupsExactTextAndOrders(t *testing.T) {
	st := openTestStore(t)
	seedSummaryRecords(t, st)

	summary, err := st.SummarizeDeliveryFailures(context.Background(), mustSummary(t, func(*delivery.SummaryInput) {}))
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	want := []FailureSummaryGroup{
		{FailureReason: "provider timeout", AttemptCount: 4},
		{FailureReason: "HTTP 503", AttemptCount: 2},
		{FailureReason: "bounce", AttemptCount: 2},
		{FailureReason: " provider timeout", AttemptCount: 1},
		{FailureReason: "http 503", AttemptCount: 1},
	}
	if len(summary.Groups) != len(want) {
		t.Fatalf("groups = %+v, want %+v", summary.Groups, want)
	}
	for i, group := range want {
		if summary.Groups[i] != group {
			t.Fatalf("group %d = %+v, want %+v", i, summary.Groups[i], group)
		}
	}
	if summary.TotalAttempts != 10 {
		t.Fatalf("total attempts = %d, want 10", summary.TotalAttempts)
	}
}

func TestSummarizeDeliveryFailuresFiltersTemplate(t *testing.T) {
	st := openTestStore(t)
	seedSummaryRecords(t, st)

	summary, err := st.SummarizeDeliveryFailures(context.Background(), mustSummary(t, func(in *delivery.SummaryInput) {
		templateID := "tpl-2"
		in.TemplateID = &templateID
	}))
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if len(summary.Groups) != 1 || summary.Groups[0].FailureReason != "provider timeout" ||
		summary.Groups[0].AttemptCount != 1 || summary.TotalAttempts != 1 {
		t.Fatalf("summary = %+v, want single tpl-2 group", summary)
	}
}

func TestSummarizeDeliveryFailuresEmptyMatch(t *testing.T) {
	st := openTestStore(t)
	seedSummaryRecords(t, st)

	summary, err := st.SummarizeDeliveryFailures(context.Background(), mustSummary(t, func(in *delivery.SummaryInput) {
		templateID := "missing"
		in.TemplateID = &templateID
	}))
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.TotalAttempts != 0 {
		t.Fatalf("total attempts = %d, want 0", summary.TotalAttempts)
	}
	if summary.Groups == nil || len(summary.Groups) != 0 {
		t.Fatalf("groups = %+v, want empty non-nil slice", summary.Groups)
	}
}
