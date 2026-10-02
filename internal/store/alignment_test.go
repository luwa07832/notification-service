package store

import (
	"context"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedAlignmentStore(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	templates := []struct {
		id       string
		channels []string
		enabled  bool
	}{
		{"tpl-ok", []string{"sms"}, true},
		{"tpl-email-only", []string{"email"}, true},
		{"tpl-disabled", []string{"sms"}, false},
		{"tpl-disabled-email", []string{"email"}, false},
	}
	for _, tpl := range templates {
		if err := st.CreateNotificationTemplate(ctx, mustTemplate(t, templateInput(tpl.id, tpl.channels, tpl.enabled))); err != nil {
			t.Fatalf("create template %s: %v", tpl.id, err)
		}
	}

	records := []delivery.RecordInput{
		{TemplateID: "tpl-ok", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-gone", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "template since removed"},
		{TemplateID: "tpl-email-only", Channel: "sms", OccurredAt: "2026-10-01T10:00:10Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "wrong channel"},
		{TemplateID: "tpl-disabled", Channel: "sms", OccurredAt: "2026-10-01T10:00:15Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "template off"},
		{TemplateID: "tpl-disabled-email", Channel: "sms", OccurredAt: "2026-10-01T10:00:20Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(3), FailureReason: "unsupported wins over disabled"},
		{TemplateID: "tpl-gone", Channel: "email", OccurredAt: "2026-10-01T10:00:25Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "other channel"},
	}
	for i, in := range records {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create record %d: %v", i, err)
		}
	}
}

func mustAlignment(t *testing.T, mutate func(*delivery.AlignmentInput)) delivery.AlignmentQuery {
	t.Helper()
	in := delivery.AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
	mutate(&in)
	query, err := delivery.NewAlignment(in)
	if err != nil {
		t.Fatalf("new alignment: %v", err)
	}
	return query
}

func TestTemplateAlignmentIssueCodesAndPriority(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentStore(t, st)

	result, err := st.TemplateAlignment(context.Background(), mustAlignment(t, func(in *delivery.AlignmentInput) {}))
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 4 {
		t.Fatalf("total = %d, want 4", result.Total)
	}
	want := []struct {
		templateID string
		issueCode  string
	}{
		{"tpl-gone", delivery.IssueTemplateMissing},
		{"tpl-email-only", delivery.IssueChannelUnsupported},
		{"tpl-disabled", delivery.IssueTemplateDisabled},
		{"tpl-disabled-email", delivery.IssueChannelUnsupported},
	}
	if len(result.Issues) != len(want) {
		t.Fatalf("issues len = %d, want %d", len(result.Issues), len(want))
	}
	for i, w := range want {
		got := result.Issues[i]
		if got.TemplateID != w.templateID || got.IssueCode != w.issueCode {
			t.Fatalf("issue %d = %s/%s, want %s/%s", i, got.TemplateID, got.IssueCode, w.templateID, w.issueCode)
		}
		if got.Channel != "sms" || got.ID == "" {
			t.Fatalf("issue %d missing record fields: %+v", i, got)
		}
	}
}

func TestTemplateAlignmentKeepsRecordFields(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentStore(t, st)

	result, err := st.TemplateAlignment(context.Background(), mustAlignment(t, func(in *delivery.AlignmentInput) {
		templateID := "tpl-disabled"
		in.TemplateID = &templateID
	}))
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 1 || len(result.Issues) != 1 {
		t.Fatalf("total/len = %d/%d, want 1/1", result.Total, len(result.Issues))
	}
	issue := result.Issues[0]
	if issue.Status != delivery.StatusRetrying || issue.RetryCount != 1 ||
		issue.FailureReason != "template off" {
		t.Fatalf("record fields changed: %+v", issue)
	}
	if issue.OccurredAt.UTC().Format("2006-01-02T15:04:05Z") != "2026-10-01T10:00:15Z" {
		t.Fatalf("occurred at = %s", issue.OccurredAt)
	}
}

func TestTemplateAlignmentPagination(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentStore(t, st)

	query := mustAlignment(t, func(in *delivery.AlignmentInput) {
		in.Page = strPtrAlignment("2")
		in.PageSize = strPtrAlignment("2")
	})
	result, err := st.TemplateAlignment(context.Background(), query)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 4 {
		t.Fatalf("total = %d, want pre-pagination 4", result.Total)
	}
	if len(result.Issues) != 2 {
		t.Fatalf("page len = %d, want 2", len(result.Issues))
	}
	if result.Issues[0].TemplateID != "tpl-disabled" || result.Issues[1].TemplateID != "tpl-disabled-email" {
		t.Fatalf("page 2 order wrong: %+v", result.Issues)
	}
}

func TestTemplateAlignmentRespectsChannelRangeAndEmpty(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentStore(t, st)

	emailQuery := mustAlignment(t, func(in *delivery.AlignmentInput) {
		in.Channel = "email"
	})
	emailResult, err := st.TemplateAlignment(context.Background(), emailQuery)
	if err != nil {
		t.Fatalf("email alignment: %v", err)
	}
	if emailResult.Total != 1 || len(emailResult.Issues) != 1 ||
		emailResult.Issues[0].IssueCode != delivery.IssueTemplateMissing {
		t.Fatalf("email result = %+v, want one missing-template issue", emailResult)
	}

	outOfRange := mustAlignment(t, func(in *delivery.AlignmentInput) {
		in.Start = "2026-10-02T10:00:00Z"
		in.End = "2026-10-02T11:00:00Z"
	})
	emptyResult, err := st.TemplateAlignment(context.Background(), outOfRange)
	if err != nil {
		t.Fatalf("empty alignment: %v", err)
	}
	if emptyResult.Total != 0 || len(emptyResult.Issues) != 0 {
		t.Fatalf("empty result = %+v, want no issues", emptyResult)
	}
}

func strPtrAlignment(v string) *string { return &v }
