package store

import (
	"context"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// seedOverviewRecords registers one fixed data set and returns the two tpl-2
// records that share the same occurred_at, so tests can verify the
// last-attempt id tie-break against the generated ids.
func seedOverviewRecords(t *testing.T, st *Store) (delivery.Record, delivery.Record) {
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
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:00:25Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "blocked"},
		{TemplateID: "tpl-10", Channel: "sms", OccurredAt: "2026-10-01T10:00:30Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(3)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T11:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "at end boundary"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "Timeout"},
	}
	var tieA, tieB delivery.Record
	for i, in := range inputs {
		saved, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in))
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if in.TemplateID == "tpl-2" && in.OccurredAt == "2026-10-01T10:00:25Z" {
			if tieA.ID == "" {
				tieA = saved
			} else {
				tieB = saved
			}
		}
	}
	return tieA, tieB
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

func findGroup(t *testing.T, groups []delivery.OverviewGroup, templateID string) delivery.OverviewGroup {
	t.Helper()
	for _, group := range groups {
		if group.TemplateID == templateID {
			return group
		}
	}
	t.Fatalf("group %s missing in %+v", templateID, groups)
	return delivery.OverviewGroup{}
}

func TestAttemptOverviewGroupsInTemplateOrder(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)

	overview, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	wantOrder := []string{"tpl-1", "tpl-10", "tpl-2"}
	if len(overview.Groups) != len(wantOrder) {
		t.Fatalf("groups = %+v, want %d groups", overview.Groups, len(wantOrder))
	}
	for i, templateID := range wantOrder {
		if overview.Groups[i].TemplateID != templateID {
			t.Fatalf("group %d template = %s, want %s", i, overview.Groups[i].TemplateID, templateID)
		}
	}
}

func TestAttemptOverviewAggregatesOneTemplate(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)

	overview, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	group := findGroup(t, overview.Groups, "tpl-1")
	if group.TotalAttempts != 4 {
		t.Fatalf("total = %d, want 4", group.TotalAttempts)
	}
	if group.RetryCountMin != 0 || group.RetryCountMax != 2 {
		t.Fatalf("retry span = %d-%d, want 0-2", group.RetryCountMin, group.RetryCountMax)
	}
	wantCounts := delivery.StatusCounts{Pending: 1, Retrying: 1, Succeeded: 0, Failed: 2}
	if group.StatusCounts != wantCounts {
		t.Fatalf("status counts = %+v, want %+v", group.StatusCounts, wantCounts)
	}
	if group.LastAttempt.OccurredAt.Format("2006-01-02T15:04:05Z") != "2026-10-01T10:00:15Z" ||
		group.LastAttempt.Status != delivery.StatusPending ||
		group.LastAttempt.RetryCount != 0 ||
		group.LastAttempt.FailureReason != "" {
		t.Fatalf("last attempt = %+v", group.LastAttempt)
	}
	wantReasons := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "timeout", AttemptCount: 1},
	}
	if len(group.FailureReasons) != len(wantReasons) {
		t.Fatalf("failure reasons = %+v, want %+v", group.FailureReasons, wantReasons)
	}
	for i := range wantReasons {
		if group.FailureReasons[i] != wantReasons[i] {
			t.Fatalf("reason %d = %+v, want %+v", i, group.FailureReasons[i], wantReasons[i])
		}
	}
}

func TestAttemptOverviewLastAttemptBreaksTieByID(t *testing.T) {
	st := openTestStore(t)
	tieA, tieB := seedOverviewRecords(t, st)

	overview, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	group := findGroup(t, overview.Groups, "tpl-2")
	ties := map[string]delivery.Record{tieA.ID: tieA, tieB.ID: tieB}
	wantID := tieA.ID
	if tieB.ID > wantID {
		wantID = tieB.ID
	}
	want := ties[wantID]
	if group.LastAttempt.ID != want.ID ||
		group.LastAttempt.Status != want.Status ||
		group.LastAttempt.RetryCount != want.RetryCount ||
		group.LastAttempt.FailureReason != want.FailureReason {
		t.Fatalf("last attempt = %+v, want record %+v", group.LastAttempt, want)
	}
	if group.TotalAttempts != 3 || group.RetryCountMin != 0 || group.RetryCountMax != 2 {
		t.Fatalf("group = %+v", group)
	}
	wantCounts := delivery.StatusCounts{Pending: 0, Retrying: 1, Succeeded: 1, Failed: 1}
	if group.StatusCounts != wantCounts {
		t.Fatalf("status counts = %+v, want %+v", group.StatusCounts, wantCounts)
	}
	wantReasons := []delivery.FailureGroup{
		{FailureReason: "blocked", AttemptCount: 1},
		{FailureReason: "bounce", AttemptCount: 1},
	}
	if len(group.FailureReasons) != len(wantReasons) ||
		group.FailureReasons[0] != wantReasons[0] ||
		group.FailureReasons[1] != wantReasons[1] {
		t.Fatalf("failure reasons = %+v, want %+v", group.FailureReasons, wantReasons)
	}
}

func TestAttemptOverviewEmptyFailureReasonsStayEmptyArray(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)

	overview, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	group := findGroup(t, overview.Groups, "tpl-10")
	if group.TotalAttempts != 1 || group.RetryCountMin != 3 || group.RetryCountMax != 3 {
		t.Fatalf("group = %+v", group)
	}
	if group.FailureReasons == nil || len(group.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %+v, want empty non-nil", group.FailureReasons)
	}
}

func TestAttemptOverviewFiltersByTemplateID(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)

	templateID := "tpl-1"
	overview, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {
		in.TemplateID = &templateID
	}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	if len(overview.Groups) != 1 || overview.Groups[0].TemplateID != "tpl-1" {
		t.Fatalf("groups = %+v, want only tpl-1", overview.Groups)
	}
}

func TestAttemptOverviewEmptyResult(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)

	overview, err := st.AttemptOverview(context.Background(), mustOverview(t, func(in *delivery.OverviewInput) {
		in.Channel = "push"
	}))
	if err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	if overview.Groups == nil || len(overview.Groups) != 0 {
		t.Fatalf("groups = %+v, want empty non-nil", overview.Groups)
	}
}

func TestAttemptOverviewDoesNotModifyRecords(t *testing.T) {
	st := openTestStore(t)
	seedOverviewRecords(t, st)
	ctx := context.Background()

	before, err := st.ListDeliveryRecords(ctx, delivery.Query{
		Channel: "sms",
		Start:   mustOverview(t, func(in *delivery.OverviewInput) {}).Start,
		End:     mustOverview(t, func(in *delivery.OverviewInput) {}).End,
	})
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	if _, err := st.AttemptOverview(ctx, mustOverview(t, func(in *delivery.OverviewInput) {})); err != nil {
		t.Fatalf("attempt overview: %v", err)
	}
	after, err := st.ListDeliveryRecords(ctx, delivery.Query{
		Channel: "sms",
		Start:   mustOverview(t, func(in *delivery.OverviewInput) {}).Start,
		End:     mustOverview(t, func(in *delivery.OverviewInput) {}).End,
	})
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("record count changed: %d before, %d after", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("record %d changed: %+v -> %+v", i, before[i], after[i])
		}
	}
}
