package store

import (
	"context"
	"errors"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/template"
)

func mustTemplate(t *testing.T, in template.Input) template.Template {
	t.Helper()
	tpl, err := template.New(in)
	if err != nil {
		t.Fatalf("new template: %v", err)
	}
	return tpl
}

func templateInput(id string, channels []string, enabled bool) template.Input {
	return template.Input{
		TemplateID: id,
		Name:       "Name " + id,
		Body:       "body of " + id,
		Channels:   channels,
		Enabled:    boolTemplatePtr(enabled),
	}
}

func boolTemplatePtr(v bool) *bool { return &v }

func TestCreateAndGetNotificationTemplateRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	want := mustTemplate(t, templateInput("tpl-1", []string{"sms", "email"}, true))
	if err := st.CreateNotificationTemplate(ctx, want); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, ok, err := st.GetNotificationTemplate(ctx, "tpl-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.TemplateID != want.TemplateID || got.Name != want.Name || got.Body != want.Body ||
		got.Enabled != want.Enabled || len(got.Channels) != len(want.Channels) {
		t.Fatalf("round trip mismatch:\n got=%#v\nwant=%#v", got, want)
	}
	for i, channel := range want.Channels {
		if got.Channels[i] != channel {
			t.Fatalf("round trip mismatch:\n got=%#v\nwant=%#v", got, want)
		}
	}
	if got.Name != "Name tpl-1" || got.Body != "body of tpl-1" || !got.Enabled {
		t.Fatalf("unexpected content: %#v", got)
	}
	if len(got.Channels) != 2 || got.Channels[0] != "sms" || got.Channels[1] != "email" {
		t.Fatalf("channels not preserved: %#v", got.Channels)
	}
}

func TestCreateNotificationTemplateDuplicate(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	first := mustTemplate(t, templateInput("tpl-1", []string{"sms"}, true))
	if err := st.CreateNotificationTemplate(ctx, first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := mustTemplate(t, template.Input{
		TemplateID: "tpl-1",
		Name:       "Different",
		Body:       "different body",
		Channels:   []string{"push"},
		Enabled:    boolTemplatePtr(false),
	})
	err := st.CreateNotificationTemplate(ctx, second)
	if !errors.Is(err, ErrTemplateAlreadyExists) {
		t.Fatalf("err = %v, want ErrTemplateAlreadyExists", err)
	}

	got, ok, err := st.GetNotificationTemplate(ctx, "tpl-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Name != "Name tpl-1" || len(got.Channels) != 1 || got.Channels[0] != "sms" || !got.Enabled {
		t.Fatalf("duplicate registration changed stored template: %#v", got)
	}
}

func TestGetNotificationTemplateMissing(t *testing.T) {
	st := openTestStore(t)
	_, ok, err := st.GetNotificationTemplate(context.Background(), "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("missing template reported as present")
	}
}

func TestListNotificationTemplatesOrdersAndFilters(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for _, tc := range []struct {
		id       string
		channels []string
		enabled  bool
	}{
		{"tpl-b", []string{"email", "push"}, false},
		{"tpl-a", []string{"sms", "email"}, true},
		{"tpl-c", []string{"sms"}, false},
	} {
		if err := st.CreateNotificationTemplate(ctx, mustTemplate(t, templateInput(tc.id, tc.channels, tc.enabled))); err != nil {
			t.Fatalf("create %s: %v", tc.id, err)
		}
	}

	assertIDs := func(t *testing.T, filter template.Filter, want []string) {
		t.Helper()
		got, err := st.ListNotificationTemplates(ctx, filter)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("got %d templates %#v, want %v", len(got), got, want)
		}
		for i, id := range want {
			if got[i].TemplateID != id {
				t.Fatalf("position %d = %s, want %s (%#v)", i, got[i].TemplateID, id, got)
			}
		}
	}

	assertIDs(t, template.Filter{}, []string{"tpl-a", "tpl-b", "tpl-c"})

	channel := "sms"
	assertIDs(t, template.Filter{Channel: &channel}, []string{"tpl-a", "tpl-c"})

	enabledFalse := false
	assertIDs(t, template.Filter{Enabled: &enabledFalse}, []string{"tpl-b", "tpl-c"})

	channel = "email"
	enabledTrue := true
	assertIDs(t, template.Filter{Channel: &channel, Enabled: &enabledTrue}, []string{"tpl-a"})

	channel = "push"
	assertIDs(t, template.Filter{Channel: &channel, Enabled: &enabledTrue}, nil)
}

func TestListNotificationTemplatesEmpty(t *testing.T) {
	st := openTestStore(t)
	got, err := st.ListNotificationTemplates(context.Background(), template.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no templates, got %#v", got)
	}
}

func TestTemplateDeliverySummaryAggregatesFacts(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateNotificationTemplate(ctx, mustTemplate(t, templateInput("tpl-1", []string{"sms"}, true))); err != nil {
		t.Fatalf("create template: %v", err)
	}

	postRecord := func(status string, retry int, reason string) {
		t.Helper()
		record := mustRecord(t, delivery.RecordInput{
			TemplateID:    "tpl-1",
			Channel:       "sms",
			OccurredAt:    "2026-10-01T10:00:00Z",
			Status:        status,
			RetryCount:    ptrInt(retry),
			FailureReason: reason,
		})
		if _, err := st.CreateDeliveryRecord(ctx, record); err != nil {
			t.Fatalf("create record: %v", err)
		}
	}
	postRecord(delivery.StatusPending, 0, "")
	postRecord(delivery.StatusRetrying, 1, "Timeout")
	postRecord(delivery.StatusSucceeded, 2, "")
	postRecord(delivery.StatusFailed, 5, "bounce")
	postRecord(delivery.StatusFailed, 3, "Timeout")

	summary, err := st.TemplateDeliverySummary(ctx, "tpl-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TotalAttempts != 5 {
		t.Fatalf("total = %d, want 5", summary.TotalAttempts)
	}
	if summary.StatusCounts != (delivery.StatusCounts{Pending: 1, Retrying: 1, Succeeded: 1, Failed: 2}) {
		t.Fatalf("status counts = %#v", summary.StatusCounts)
	}
	if summary.RetryCountMin == nil || *summary.RetryCountMin != 0 {
		t.Fatalf("min = %v, want 0", summary.RetryCountMin)
	}
	if summary.RetryCountMax == nil || *summary.RetryCountMax != 5 {
		t.Fatalf("max = %v, want 5", summary.RetryCountMax)
	}
	wantReasons := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "bounce", AttemptCount: 1},
	}
	if len(summary.FailureReasons) != len(wantReasons) {
		t.Fatalf("reasons = %#v", summary.FailureReasons)
	}
	for i, want := range wantReasons {
		if summary.FailureReasons[i] != want {
			t.Fatalf("reason %d = %#v, want %#v", i, summary.FailureReasons[i], want)
		}
	}
}

func TestTemplateDeliverySummaryWithoutAttempts(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateNotificationTemplate(ctx, mustTemplate(t, templateInput("tpl-1", []string{"sms"}, true))); err != nil {
		t.Fatalf("create template: %v", err)
	}

	summary, err := st.TemplateDeliverySummary(ctx, "tpl-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TotalAttempts != 0 {
		t.Fatalf("total = %d, want 0", summary.TotalAttempts)
	}
	if summary.StatusCounts != (delivery.StatusCounts{}) {
		t.Fatalf("status counts = %#v", summary.StatusCounts)
	}
	if summary.RetryCountMin != nil || summary.RetryCountMax != nil {
		t.Fatalf("bounds = %v/%v, want nil/nil", summary.RetryCountMin, summary.RetryCountMax)
	}
	if len(summary.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %#v, want empty", summary.FailureReasons)
	}
}

func TestTemplateDeliverySummaryOnlyCountsMatchingTemplate(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"tpl-1", "tpl-2"} {
		if err := st.CreateNotificationTemplate(ctx, mustTemplate(t, templateInput(id, []string{"sms"}, true))); err != nil {
			t.Fatalf("create template: %v", err)
		}
	}
	for _, id := range []string{"tpl-1", "tpl-2"} {
		record := mustRecord(t, delivery.RecordInput{
			TemplateID: id, Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "boom",
		})
		if _, err := st.CreateDeliveryRecord(ctx, record); err != nil {
			t.Fatalf("create record: %v", err)
		}
	}
	summary, err := st.TemplateDeliverySummary(ctx, "tpl-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TotalAttempts != 1 || len(summary.FailureReasons) != 1 ||
		summary.FailureReasons[0].AttemptCount != 1 {
		t.Fatalf("summary leaked other template rows: %#v", summary)
	}
}
