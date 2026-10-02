package store

import (
	"context"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedComparisonRecords(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-2", Channel: "email", OccurredAt: "2026-10-01T10:00:20Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(3), FailureReason: "bounce"},
		{TemplateID: "tpl-2", Channel: "email", OccurredAt: "2026-10-01T10:00:25Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-2", Channel: "push", OccurredAt: "2026-10-01T10:00:30Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T11:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "at end boundary"},
		{TemplateID: "tpl-9", Channel: "email", OccurredAt: "2026-10-01T10:00:40Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(5), FailureReason: "Timeout"},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
}

func mustComparison(t *testing.T, mutate func(*delivery.ComparisonInput)) delivery.ComparisonQuery {
	t.Helper()
	in := delivery.ComparisonInput{
		Channels: "sms,email,push",
		Start:    "2026-10-01T10:00:00Z",
		End:      "2026-10-01T11:00:00Z",
	}
	mutate(&in)
	query, err := delivery.NewComparison(in)
	if err != nil {
		t.Fatalf("new comparison: %v", err)
	}
	return query
}

func TestChannelComparisonAggregatesPerChannel(t *testing.T) {
	st := openTestStore(t)
	seedComparisonRecords(t, st)

	result, err := st.ChannelComparison(context.Background(), mustComparison(t, func(in *delivery.ComparisonInput) {}))
	if err != nil {
		t.Fatalf("channel comparison: %v", err)
	}

	wantChannels := []string{"sms", "email", "push"}
	if len(result.Channels) != 3 {
		t.Fatalf("channels = %v", result.Channels)
	}
	for i, want := range wantChannels {
		if result.Channels[i] != want {
			t.Fatalf("channel order = %v", result.Channels)
		}
	}
	if result.TotalAttempts["sms"] != 3 || result.TotalAttempts["email"] != 3 || result.TotalAttempts["push"] != 1 {
		t.Fatalf("total attempts = %v", result.TotalAttempts)
	}
	smsCounts := result.StatusCounts["sms"]
	if smsCounts.Pending != 1 || smsCounts.Retrying != 1 || smsCounts.Succeeded != 0 || smsCounts.Failed != 1 {
		t.Fatalf("sms status counts = %+v", smsCounts)
	}
	emailCounts := result.StatusCounts["email"]
	if emailCounts.Pending != 0 || emailCounts.Succeeded != 1 || emailCounts.Failed != 2 {
		t.Fatalf("email status counts = %+v", emailCounts)
	}
	if *result.RetryCountMin["sms"] != 0 || *result.RetryCountMax["sms"] != 2 {
		t.Fatalf("sms retry bounds = %v %v", result.RetryCountMin["sms"], result.RetryCountMax["sms"])
	}
	if *result.RetryCountMin["email"] != 0 || *result.RetryCountMax["email"] != 5 {
		t.Fatalf("email retry bounds = %v %v", result.RetryCountMin["email"], result.RetryCountMax["email"])
	}
}

func TestChannelComparisonFailureReasonsVerbatimAndOrdered(t *testing.T) {
	st := openTestStore(t)
	seedComparisonRecords(t, st)

	result, err := st.ChannelComparison(context.Background(), mustComparison(t, func(in *delivery.ComparisonInput) {}))
	if err != nil {
		t.Fatalf("channel comparison: %v", err)
	}

	reasons := result.FailureReasons
	if len(reasons) != 3 {
		t.Fatalf("failure reasons = %+v", reasons)
	}
	if reasons[0].FailureReason != "Timeout" || reasons[0].AttemptCount != 3 {
		t.Fatalf("first reason = %+v, want Timeout total 3", reasons[0])
	}
	if reasons[0].ChannelCounts["sms"] != 2 || reasons[0].ChannelCounts["email"] != 1 || reasons[0].ChannelCounts["push"] != 0 {
		t.Fatalf("Timeout channel counts = %v", reasons[0].ChannelCounts)
	}
	if reasons[1].FailureReason != "bounce" || reasons[1].AttemptCount != 1 {
		t.Fatalf("second reason = %+v, want bounce", reasons[1])
	}
	if reasons[2].FailureReason != "timeout" || reasons[2].AttemptCount != 1 {
		t.Fatalf("third reason = %+v, want lowercase timeout kept distinct", reasons[2])
	}
}

func TestChannelComparisonFiltersTemplateIDAndRange(t *testing.T) {
	st := openTestStore(t)
	seedComparisonRecords(t, st)

	templateID := "tpl-1"
	result, err := st.ChannelComparison(context.Background(), mustComparison(t, func(in *delivery.ComparisonInput) {
		in.TemplateID = &templateID
	}))
	if err != nil {
		t.Fatalf("channel comparison: %v", err)
	}
	if result.TotalAttempts["sms"] != 3 || result.TotalAttempts["email"] != 0 || result.TotalAttempts["push"] != 0 {
		t.Fatalf("total attempts = %v", result.TotalAttempts)
	}
	if len(result.FailureReasons) != 1 || result.FailureReasons[0].FailureReason != "Timeout" || result.FailureReasons[0].AttemptCount != 2 {
		t.Fatalf("failure reasons = %+v", result.FailureReasons)
	}
}

func TestChannelComparisonEmptyRangeKeepsAllChannels(t *testing.T) {
	st := openTestStore(t)
	seedComparisonRecords(t, st)

	result, err := st.ChannelComparison(context.Background(), mustComparison(t, func(in *delivery.ComparisonInput) {
		in.Start = "2026-09-01T00:00:00Z"
		in.End = "2026-09-02T00:00:00Z"
	}))
	if err != nil {
		t.Fatalf("channel comparison: %v", err)
	}
	for _, channel := range []string{"sms", "email", "push"} {
		if result.TotalAttempts[channel] != 0 {
			t.Fatalf("total for %s = %d, want 0", channel, result.TotalAttempts[channel])
		}
		if _, ok := result.StatusCounts[channel]; !ok {
			t.Fatalf("status counts missing channel %s", channel)
		}
		if result.RetryCountMin[channel] != nil || result.RetryCountMax[channel] != nil {
			t.Fatalf("retry bounds for %s must be null", channel)
		}
	}
	if len(result.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %+v, want empty", result.FailureReasons)
	}
}
