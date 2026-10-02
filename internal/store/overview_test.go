package store

import (
	"context"
	"testing"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedOverviewRecords(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:15Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:20Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(3), FailureReason: "bounce"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:25Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "bounce"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:30Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(2), FailureReason: "Timeout"},
		{TemplateID: "tpl-3", Channel: "sms", OccurredAt: "2026-10-01T10:00:35Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T11:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "at end boundary"},
		{TemplateID: "tpl-9", Channel: "sms", OccurredAt: "2026-10-01T09:59:59Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "before start"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "Timeout"},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
}

func mustOverview(t *testing.T, mutate func(*delivery.OverviewInput)) delivery.OverviewQuery {
	t.Helper()
	in := delivery.OverviewInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
	mutate(&in)
	query, err := delivery.NewOverview(in)
	if err != nil {
		t.Fatalf("new overview: %v", err)
	}
	return query
}

func overviewGroupByTemplate(t *testing.T, groups []delivery.OverviewGroup, templateID string) delivery.OverviewGroup {
	t.Helper()
	for _, group := range groups {
		if group.TemplateID == templateID {
			return group
		}
	}
	t.Fatalf("no group for %s in %+v", templateID, groups)
	return delivery.OverviewGroup{}
}

func TestAttemptOverviewAggregatesPerTemplate(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)
	groups, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	if len(groups) != 3 || groups[0].TemplateID != "tpl-1" || groups[1].TemplateID != "tpl-2" || groups[2].TemplateID != "tpl-3" {
		t.Fatalf("groups = %+v", groups)
	}

	first := overviewGroupByTemplate(t, groups, "tpl-1")
	if first.TotalAttempts != 4 || first.RetryCountMin != 0 || first.RetryCountMax != 2 {
		t.Fatalf("tpl-1 totals = %+v", first)
	}
	wantCounts := delivery.StatusCounts{Pending: 1, Retrying: 1, Succeeded: 1, Failed: 1}
	if first.StatusCounts != wantCounts {
		t.Fatalf("tpl-1 status counts = %+v, want %+v", first.StatusCounts, wantCounts)
	}
	if first.LastAttempt.Status != delivery.StatusSucceeded ||
		first.LastAttempt.OccurredAt != time.Date(2026, 10, 1, 10, 0, 15, 0, time.UTC) ||
		first.LastAttempt.RetryCount != 0 || first.LastAttempt.FailureReason != "" || first.LastAttempt.ID == "" {
		t.Fatalf("tpl-1 last attempt = %+v", first.LastAttempt)
	}
	wantReasons := []delivery.FailureGroup{{FailureReason: "Timeout", AttemptCount: 2}}
	if len(first.FailureReasons) != 1 || first.FailureReasons[0] != wantReasons[0] {
		t.Fatalf("tpl-1 reasons = %+v, want %+v", first.FailureReasons, wantReasons)
	}

	second := overviewGroupByTemplate(t, groups, "tpl-2")
	if second.TotalAttempts != 3 || second.RetryCountMin != 1 || second.RetryCountMax != 3 {
		t.Fatalf("tpl-2 totals = %+v", second)
	}
	wantCounts = delivery.StatusCounts{Retrying: 1, Failed: 2}
	if second.StatusCounts != wantCounts {
		t.Fatalf("tpl-2 status counts = %+v, want %+v", second.StatusCounts, wantCounts)
	}
	if second.LastAttempt.Status != delivery.StatusRetrying || second.LastAttempt.FailureReason != "Timeout" {
		t.Fatalf("tpl-2 last attempt = %+v", second.LastAttempt)
	}
	wantReasons = []delivery.FailureGroup{
		{FailureReason: "bounce", AttemptCount: 2},
		{FailureReason: "Timeout", AttemptCount: 1},
	}
	if len(second.FailureReasons) != 2 || second.FailureReasons[0] != wantReasons[0] || second.FailureReasons[1] != wantReasons[1] {
		t.Fatalf("tpl-2 reasons = %+v, want %+v", second.FailureReasons, wantReasons)
	}

	third := overviewGroupByTemplate(t, groups, "tpl-3")
	if third.TotalAttempts != 1 || third.StatusCounts != (delivery.StatusCounts{Succeeded: 1}) {
		t.Fatalf("tpl-3 group = %+v", third)
	}
	if third.FailureReasons == nil || len(third.FailureReasons) != 0 {
		t.Fatalf("tpl-3 reasons = %+v, want empty non-nil", third.FailureReasons)
	}
}

func TestAttemptOverviewLastAttemptBreaksTiesOnID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	first, err := st.CreateDeliveryRecord(ctx, mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-tie", Channel: "sms", OccurredAt: "2026-10-01T10:01:00Z",
		Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "tie one",
	}))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := st.CreateDeliveryRecord(ctx, mustRecord(t, delivery.RecordInput{
		TemplateID: "tpl-tie", Channel: "sms", OccurredAt: "2026-10-01T10:01:00Z",
		Status: delivery.StatusRetrying, RetryCount: ptrInt(2), FailureReason: "tie two",
	}))
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	want := first
	if second.ID > first.ID {
		want = second
	}
	groups, err := st.AttemptOverview(ctx, mustOverview(t, func(in *delivery.OverviewInput) {}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want one", groups)
	}
	last := groups[0].LastAttempt
	if last.ID != want.ID || last.Status != want.Status ||
		last.RetryCount != want.RetryCount || last.FailureReason != want.FailureReason {
		t.Fatalf("last attempt = %+v, want record %+v", last, want)
	}
}

func TestAttemptOverviewFiltersByTemplateID(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)
	templateID := "tpl-2"
	groups, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {
		in.TemplateID = &templateID
	}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	if len(groups) != 1 || groups[0].TemplateID != "tpl-2" || groups[0].TotalAttempts != 3 {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestAttemptOverviewEmptyResult(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)
	groups, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {
		in.Channel = "push"
	}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	if groups == nil || len(groups) != 0 {
		t.Fatalf("groups = %+v, want empty non-nil", groups)
	}
}
