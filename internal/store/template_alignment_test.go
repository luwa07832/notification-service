package store

import (
	"context"
	"testing"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedAlignmentFixture(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	templates := []struct {
		id       string
		channels []string
		enabled  bool
	}{
		{"tpl-sms-on", []string{"sms"}, true},
		{"tpl-email-on", []string{"email"}, true},
		{"tpl-sms-off", []string{"sms"}, false},
	}
	for _, tmpl := range templates {
		if err := st.CreateNotificationTemplate(ctx,
			mustTemplate(t, templateInput(tmpl.id, tmpl.channels, tmpl.enabled))); err != nil {
			t.Fatalf("create template %s: %v", tmpl.id, err)
		}
	}

	records := []delivery.RecordInput{
		{TemplateID: "tpl-missing", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "missing template"},
		{TemplateID: "tpl-email-on", Channel: "sms", OccurredAt: "2026-10-01T10:05:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "wrong channel"},
		{TemplateID: "tpl-sms-off", Channel: "sms", OccurredAt: "2026-10-01T10:10:00Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-sms-on", Channel: "sms", OccurredAt: "2026-10-01T10:15:00Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-missing", Channel: "sms", OccurredAt: "2026-10-01T11:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(3), FailureReason: "at end bound"},
		{TemplateID: "tpl-missing", Channel: "email", OccurredAt: "2026-10-01T10:20:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "other channel"},
	}
	for i, in := range records {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create record %d: %v", i, err)
		}
	}
}

func mustAlignment(t *testing.T, in delivery.AlignmentInput) delivery.AlignmentQuery {
	t.Helper()
	query, err := delivery.NewAlignment(in)
	if err != nil {
		t.Fatalf("new alignment: %v", err)
	}
	return query
}

func issueCodes(issues []delivery.AlignmentIssue) []string {
	codes := make([]string, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.IssueCode)
	}
	return codes
}

func TestTemplateAlignmentClassifiesAndOrders(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentFixture(t, st)

	query := mustAlignment(t, delivery.AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	})
	result, err := st.TemplateAlignment(context.Background(), query)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("total = %d, want 3", result.Total)
	}
	if len(result.Issues) != 3 {
		t.Fatalf("issues = %d, want 3", len(result.Issues))
	}
	got := issueCodes(result.Issues)
	want := []string{delivery.IssueTemplateMissing, delivery.IssueChannelUnsupported, delivery.IssueTemplateDisabled}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("codes = %v, want %v", got, want)
		}
	}

	first := result.Issues[0]
	if first.TemplateID != "tpl-missing" || first.Channel != "sms" ||
		first.Status != "failed" || first.RetryCount != 1 || first.FailureReason != "missing template" {
		t.Fatalf("unexpected first issue: %+v", first)
	}
	if first.ID == "" {
		t.Fatalf("issue id must be populated")
	}
	if first.OccurredAt.IsZero() {
		t.Fatalf("issue occurred_at must be populated")
	}
}

func TestTemplateAlignmentHalfOpenRange(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentFixture(t, st)

	// The record at exactly 11:00 is excluded by the open end bound.
	query := mustAlignment(t, delivery.AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	})
	result, err := st.TemplateAlignment(context.Background(), query)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	for _, issue := range result.Issues {
		if issue.FailureReason == "at end bound" {
			t.Fatalf("record at end bound must not be returned: %+v", issue)
		}
	}

	// A range starting at 11:00 includes the end-bound record but nothing
	// earlier, and a record exactly at start is part of the range.
	atEnd := mustAlignment(t, delivery.AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T11:00:00Z",
		End:     "2026-10-01T12:00:00Z",
	})
	endResult, err := st.TemplateAlignment(context.Background(), atEnd)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if endResult.Total != 1 || len(endResult.Issues) != 1 {
		t.Fatalf("total = %d len = %d, want 1/1", endResult.Total, len(endResult.Issues))
	}
	if endResult.Issues[0].IssueCode != delivery.IssueTemplateMissing {
		t.Fatalf("code = %s", endResult.Issues[0].IssueCode)
	}
}

func TestTemplateAlignmentTemplateFilterAndChannel(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentFixture(t, st)

	tplID := "tpl-sms-off"
	query := mustAlignment(t, delivery.AlignmentInput{
		Channel:    "sms",
		Start:      "2026-10-01T10:00:00Z",
		End:        "2026-10-01T11:00:00Z",
		TemplateID: &tplID,
	})
	result, err := st.TemplateAlignment(context.Background(), query)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("total = %d, want 1", result.Total)
	}
	if result.Issues[0].TemplateID != tplID || result.Issues[0].IssueCode != delivery.IssueTemplateDisabled {
		t.Fatalf("unexpected issue: %+v", result.Issues[0])
	}

	// A consistent template filtered explicitly returns no issues.
	consistentID := "tpl-sms-on"
	consistentQuery := mustAlignment(t, delivery.AlignmentInput{
		Channel:    "sms",
		Start:      "2026-10-01T10:00:00Z",
		End:        "2026-10-01T11:00:00Z",
		TemplateID: &consistentID,
	})
	consistentResult, err := st.TemplateAlignment(context.Background(), consistentQuery)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if consistentResult.Total != 0 || len(consistentResult.Issues) != 0 {
		t.Fatalf("consistent template must produce no issues: %+v", consistentResult)
	}

	// The other channel's records never leak into an sms query.
	allSMS := mustAlignment(t, delivery.AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T09:00:00Z",
		End:     "2026-10-01T12:00:00Z",
	})
	smsResult, err := st.TemplateAlignment(context.Background(), allSMS)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if smsResult.Total != 4 {
		t.Fatalf("sms total = %d, want 4", smsResult.Total)
	}
}

func TestTemplateAlignmentPaginationAndTotal(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentFixture(t, st)

	pageSize, page := "1", "2"
	query := mustAlignment(t, delivery.AlignmentInput{
		Channel:  "sms",
		Start:    "2026-10-01T10:00:00Z",
		End:      "2026-10-01T11:00:00Z",
		Page:     &page,
		PageSize: &pageSize,
	})
	result, err := st.TemplateAlignment(context.Background(), query)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("total before pagination = %d, want 3", result.Total)
	}
	if len(result.Issues) != 1 || result.Issues[0].IssueCode != delivery.IssueChannelUnsupported {
		t.Fatalf("page 2 = %+v", result.Issues)
	}

	// A page past the last one is empty while the total stays accurate.
	lastPage := "4"
	emptyQuery := mustAlignment(t, delivery.AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
		Page:    &lastPage,
	})
	emptyResult, err := st.TemplateAlignment(context.Background(), emptyQuery)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if emptyResult.Total != 3 || len(emptyResult.Issues) != 0 {
		t.Fatalf("empty page = %+v", emptyResult)
	}
	if emptyResult.Issues == nil {
		t.Fatalf("issues should be an empty slice, not nil")
	}
}

func TestTemplateAlignmentNoMatch(t *testing.T) {
	st := openTestStore(t)
	seedAlignmentFixture(t, st)

	query := mustAlignment(t, delivery.AlignmentInput{
		Channel: "push",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	})
	result, err := st.TemplateAlignment(context.Background(), query)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("total = %d, want 0", result.Total)
	}
	if len(result.Issues) != 0 || result.Issues == nil {
		t.Fatalf("issues = %+v, want empty non-nil slice", result.Issues)
	}
}

func TestTemplateAlignmentTieBreaksByID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-z", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "z"},
		{TemplateID: "tpl-a", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "a"},
	}
	for _, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	query := mustAlignment(t, delivery.AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	})
	result, err := st.TemplateAlignment(ctx, query)
	if err != nil {
		t.Fatalf("alignment: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("total = %d, want 2", result.Total)
	}
	if result.Issues[0].ID >= result.Issues[1].ID {
		t.Fatalf("same-timestamp order = %s then %s, want id ascending",
			result.Issues[0].ID, result.Issues[1].ID)
	}
}
