package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/template"
)

func openTemplateStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sampleTemplate(id string) template.Template {
	return template.Template{
		TemplateID: id,
		Name:       "Welcome " + id,
		Body:       "hello body",
		Channels:   []string{"in_app", "sms"},
		Enabled:    true,
	}
}

func TestCreateNotificationTemplateRejectsDuplicate(t *testing.T) {
	st := openTemplateStore(t)
	ctx := context.Background()

	if err := st.CreateNotificationTemplate(ctx, sampleTemplate("tpl-1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := st.CreateNotificationTemplate(ctx, sampleTemplate("tpl-1"))
	if !errors.Is(err, ErrTemplateAlreadyExists) {
		t.Fatalf("duplicate err = %v, want ErrTemplateAlreadyExists", err)
	}
}

func TestListNotificationTemplatesFiltersAndOrder(t *testing.T) {
	st := openTemplateStore(t)
	ctx := context.Background()

	disabled := sampleTemplate("tpl-3")
	disabled.Enabled = false
	disabled.Channels = []string{"email"}
	two := sampleTemplate("tpl-2")
	two.Channels = []string{"sms"}
	one := sampleTemplate("tpl-1")
	for _, tpl := range []template.Template{disabled, two, one} {
		if err := st.CreateNotificationTemplate(ctx, tpl); err != nil {
			t.Fatalf("create %s: %v", tpl.TemplateID, err)
		}
	}

	all, err := st.ListNotificationTemplates(ctx, template.ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 || all[0].TemplateID != "tpl-1" || all[1].TemplateID != "tpl-2" || all[2].TemplateID != "tpl-3" {
		t.Fatalf("unexpected order: %+v", all)
	}
	if got := all[0].Channels; len(got) != 2 || got[0] != "in_app" || got[1] != "sms" {
		t.Fatalf("channels lost order: %v", got)
	}

	channel := "sms"
	byChannel, err := st.ListNotificationTemplates(ctx, template.ListFilter{Channel: &channel})
	if err != nil {
		t.Fatalf("list by channel: %v", err)
	}
	if len(byChannel) != 2 || byChannel[0].TemplateID != "tpl-1" || byChannel[1].TemplateID != "tpl-2" {
		t.Fatalf("channel filter result: %+v", byChannel)
	}

	enabled := false
	disabledOnly, err := st.ListNotificationTemplates(ctx, template.ListFilter{Enabled: &enabled})
	if err != nil {
		t.Fatalf("list by enabled: %v", err)
	}
	if len(disabledOnly) != 1 || disabledOnly[0].TemplateID != "tpl-3" {
		t.Fatalf("enabled filter result: %+v", disabledOnly)
	}

	none, err := st.ListNotificationTemplates(ctx, template.ListFilter{Channel: &channel, Enabled: &enabled})
	if err != nil {
		t.Fatalf("combined list: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no match, got %+v", none)
	}
}

func TestGetNotificationTemplateDetailAggregatesAttempts(t *testing.T) {
	st := openTemplateStore(t)
	ctx := context.Background()

	if err := st.CreateNotificationTemplate(ctx, sampleTemplate("tpl-1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, record := range []delivery.Record{
		{ID: "1", TemplateID: "tpl-1", Channel: "sms", Status: delivery.StatusPending, RetryCount: 0, FailureReason: ""},
		{ID: "2", TemplateID: "tpl-1", Channel: "sms", Status: delivery.StatusRetrying, RetryCount: 1, FailureReason: "Timeout"},
		{ID: "3", TemplateID: "tpl-1", Channel: "sms", Status: delivery.StatusFailed, RetryCount: 3, FailureReason: "Timeout"},
		{ID: "4", TemplateID: "tpl-1", Channel: "sms", Status: delivery.StatusFailed, RetryCount: 3, FailureReason: "bounce"},
		{ID: "5", TemplateID: "tpl-1", Channel: "sms", Status: delivery.StatusSucceeded, RetryCount: 4, FailureReason: ""},
		{ID: "6", TemplateID: "other", Channel: "sms", Status: delivery.StatusFailed, RetryCount: 9, FailureReason: "unrelated"},
	} {
		if _, err := st.CreateDeliveryRecord(ctx, record); err != nil {
			t.Fatalf("create record %s: %v", record.ID, err)
		}
	}

	detail, ok, err := st.GetNotificationTemplateDetail(ctx, "tpl-1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !ok {
		t.Fatal("template missing")
	}
	if detail.TemplateID != "tpl-1" || detail.Name != "Welcome tpl-1" || detail.Body != "hello body" {
		t.Fatalf("template fields mismatch: %+v", detail.Template)
	}
	summary := detail.DeliverySummary
	if summary.TotalAttempts != 5 {
		t.Fatalf("total = %d, want 5", summary.TotalAttempts)
	}
	if summary.StatusCounts != (delivery.StatusCounts{Pending: 1, Retrying: 1, Succeeded: 1, Failed: 2}) {
		t.Fatalf("status counts = %+v", summary.StatusCounts)
	}
	if summary.RetryCountMin == nil || *summary.RetryCountMin != 0 {
		t.Fatalf("min = %v, want 0", summary.RetryCountMin)
	}
	if summary.RetryCountMax == nil || *summary.RetryCountMax != 4 {
		t.Fatalf("max = %v, want 4", summary.RetryCountMax)
	}
	wantReasons := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "bounce", AttemptCount: 1},
	}
	if len(summary.FailureReasons) != len(wantReasons) {
		t.Fatalf("failure reasons = %+v", summary.FailureReasons)
	}
	for i, want := range wantReasons {
		if summary.FailureReasons[i] != want {
			t.Fatalf("failure reason %d = %+v, want %+v", i, summary.FailureReasons[i], want)
		}
	}
}

func TestGetNotificationTemplateDetailEmptySummary(t *testing.T) {
	st := openTemplateStore(t)
	ctx := context.Background()
	if err := st.CreateNotificationTemplate(ctx, sampleTemplate("tpl-empty")); err != nil {
		t.Fatalf("create: %v", err)
	}

	detail, ok, err := st.GetNotificationTemplateDetail(ctx, "tpl-empty")
	if err != nil || !ok {
		t.Fatalf("detail = %v ok = %v", err, ok)
	}
	summary := detail.DeliverySummary
	if summary.TotalAttempts != 0 {
		t.Fatalf("total = %d", summary.TotalAttempts)
	}
	if summary.StatusCounts != (delivery.StatusCounts{}) {
		t.Fatalf("status counts = %+v", summary.StatusCounts)
	}
	if summary.RetryCountMin != nil || summary.RetryCountMax != nil {
		t.Fatalf("bounds = %v/%v, want nil", summary.RetryCountMin, summary.RetryCountMax)
	}
	if len(summary.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %+v", summary.FailureReasons)
	}

	if _, ok, err := st.GetNotificationTemplateDetail(ctx, "missing"); err != nil || ok {
		t.Fatalf("missing detail = %v ok = %v", err, ok)
	}
}

func TestNotificationTemplateStorageUnavailable(t *testing.T) {
	st := openTemplateStore(t)
	ctx := context.Background()
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := st.CreateNotificationTemplate(ctx, sampleTemplate("tpl-1")); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("create err = %v, want ErrStorageUnavailable", err)
	}
	if _, err := st.ListNotificationTemplates(ctx, template.ListFilter{}); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("list err = %v, want ErrStorageUnavailable", err)
	}
	if _, _, err := st.GetNotificationTemplateDetail(ctx, "tpl-1"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("detail err = %v, want ErrStorageUnavailable", err)
	}
}
