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
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(2), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(3)},
		{TemplateID: "tpl-2", Channel: "email", OccurredAt: "2026-10-01T10:00:02Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "bounce"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:07Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:12Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "push", OccurredAt: "2026-10-01T10:00:04Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(5), FailureReason: "timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T11:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(9), FailureReason: "at end boundary"},
		{TemplateID: "tpl-other", Channel: "sms", OccurredAt: "2026-10-01T10:00:15Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "Timeout"},
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
	if len(result.Channels) != len(wantChannels) {
		t.Fatalf("channels = %v, want %v", result.Channels, wantChannels)
	}
	for i, want := range wantChannels {
		if result.Channels[i] != want {
			t.Fatalf("channel %d = %s, want %s", i, result.Channels[i], want)
		}
	}

	if result.TotalAttempts["sms"] != 4 || result.TotalAttempts["email"] != 3 || result.TotalAttempts["push"] != 1 {
		t.Fatalf("totals = %+v", result.TotalAttempts)
	}
	smsCounts := result.StatusCounts["sms"]
	if smsCounts != (delivery.StatusCounts{Pending: 0, Retrying: 1, Succeeded: 1, Failed: 2}) {
		t.Fatalf("sms status counts = %+v", smsCounts)
	}
	emailCounts := result.StatusCounts["email"]
	if emailCounts != (delivery.StatusCounts{Pending: 1, Retrying: 1, Succeeded: 0, Failed: 1}) {
		t.Fatalf("email status counts = %+v", emailCounts)
	}
	if *result.RetryCountMin["sms"] != 0 || *result.RetryCountMax["sms"] != 3 {
		t.Fatalf("sms retry bounds = %v %v", result.RetryCountMin["sms"], result.RetryCountMax["sms"])
	}
	if *result.RetryCountMin["push"] != 5 || *result.RetryCountMax["push"] != 5 {
		t.Fatalf("push retry bounds = %v %v", result.RetryCountMin["push"], result.RetryCountMax["push"])
	}

	if len(result.FailureReasons) != 3 {
		t.Fatalf("failure reasons = %+v", result.FailureReasons)
	}
	wantOrder := []struct {
		reason string
		total  int
		sms    int
		email  int
		push   int
	}{
		{"Timeout", 4, 3, 1, 0},
		{"bounce", 1, 0, 1, 0},
		{"timeout", 1, 0, 0, 1},
	}
	for i, want := range wantOrder {
		got := result.FailureReasons[i]
		if got.FailureReason != want.reason || got.AttemptCount != want.total {
			t.Fatalf("reason %d = %+v, want %s/%d", i, got, want.reason, want.total)
		}
		if got.ChannelCounts["sms"] != want.sms || got.ChannelCounts["email"] != want.email || got.ChannelCounts["push"] != want.push {
			t.Fatalf("reason %s channel counts = %+v", got.FailureReason, got.ChannelCounts)
		}
	}
}

func TestChannelComparisonKeepsRequestOrderAndEmptyChannel(t *testing.T) {
	st := openTestStore(t)
	seedComparisonRecords(t, st)

	query := mustComparison(t, func(in *delivery.ComparisonInput) {
		in.Channels = "push,sms,in_app"
	})
	result, err := st.ChannelComparison(context.Background(), query)
	if err != nil {
		t.Fatalf("channel comparison: %v", err)
	}
	if result.TotalAttempts["sms"] != 4 {
		t.Fatalf("sms total = %d, want 4", result.TotalAttempts["sms"])
	}
	if result.Channels[0] != "push" || result.Channels[1] != "sms" || result.Channels[2] != "in_app" {
		t.Fatalf("channels = %v", result.Channels)
	}
	if result.TotalAttempts["in_app"] != 0 {
		t.Fatalf("in_app total = %d, want 0", result.TotalAttempts["in_app"])
	}
	if result.RetryCountMin["in_app"] != nil || result.RetryCountMax["in_app"] != nil {
		t.Fatalf("empty channel retry bounds must be nil: %v %v",
			result.RetryCountMin["in_app"], result.RetryCountMax["in_app"])
	}
	if result.StatusCounts["in_app"] != (delivery.StatusCounts{}) {
		t.Fatalf("empty channel status counts = %+v", result.StatusCounts["in_app"])
	}
	timeout := findComparisonReason(t, result.FailureReasons, "Timeout")
	if timeout.ChannelCounts["in_app"] != 0 {
		t.Fatalf("missing channel must keep zero count: %+v", timeout.ChannelCounts)
	}
}

func TestChannelComparisonAppliesTemplateFilter(t *testing.T) {
	st := openTestStore(t)
	seedComparisonRecords(t, st)

	templateID := "tpl-1"
	query := mustComparison(t, func(in *delivery.ComparisonInput) {
		in.TemplateID = &templateID
	})
	result, err := st.ChannelComparison(context.Background(), query)
	if err != nil {
		t.Fatalf("channel comparison: %v", err)
	}
	if result.TotalAttempts["sms"] != 3 {
		t.Fatalf("sms total = %d, want 3 (tpl-other excluded)", result.TotalAttempts["sms"])
	}
	timeout := findComparisonReason(t, result.FailureReasons, "Timeout")
	if timeout.AttemptCount != 3 {
		t.Fatalf("Timeout total = %d, want 3", timeout.AttemptCount)
	}
}

func TestChannelComparisonEmptyRange(t *testing.T) {
	st := openTestStore(t)
	seedComparisonRecords(t, st)

	query := mustComparison(t, func(in *delivery.ComparisonInput) {
		in.Start = "2026-09-01T00:00:00Z"
		in.End = "2026-09-02T00:00:00Z"
	})
	result, err := st.ChannelComparison(context.Background(), query)
	if err != nil {
		t.Fatalf("channel comparison: %v", err)
	}
	if len(result.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %+v, want empty", result.FailureReasons)
	}
	for _, channel := range result.Channels {
		if result.TotalAttempts[channel] != 0 {
			t.Fatalf("%s total = %d, want 0", channel, result.TotalAttempts[channel])
		}
		if result.RetryCountMin[channel] != nil || result.RetryCountMax[channel] != nil {
			t.Fatalf("%s retry bounds must be nil", channel)
		}
	}
}

func findComparisonReason(t *testing.T, reasons []delivery.ComparisonFailureReason, want string) delivery.ComparisonFailureReason {
	t.Helper()
	for _, reason := range reasons {
		if reason.FailureReason == want {
			return reason
		}
	}
	t.Fatalf("failure reason %q not found in %+v", want, reasons)
	return delivery.ComparisonFailureReason{}
}
