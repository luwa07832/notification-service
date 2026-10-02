package delivery

import (
	"reflect"
	"testing"
	"time"
)

func TestNewRecordNotificationIDRules(t *testing.T) {
	valid := func() RecordInput {
		return RecordInput{
			TemplateID:    "tpl-1",
			Channel:       "sms",
			OccurredAt:    "2026-10-01T10:00:00Z",
			Status:        StatusFailed,
			RetryCount:    intPtr(1),
			FailureReason: "boom",
		}
	}

	t.Run("omitted stays untagged", func(t *testing.T) {
		record, err := NewRecord(valid())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if record.NotificationID != nil {
			t.Fatalf("notification id = %q, want nil", *record.NotificationID)
		}
	})

	t.Run("present value is kept verbatim", func(t *testing.T) {
		in := valid()
		in.NotificationID = strPtr("ntf-001")
		record, err := NewRecord(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if record.NotificationID == nil || *record.NotificationID != "ntf-001" {
			t.Fatalf("notification id = %v, want ntf-001", record.NotificationID)
		}
	})

	invalid := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"all whitespace", "   "},
		{"leading whitespace", " ntf-1"},
		{"trailing whitespace", "ntf-1 "},
	}
	for _, tc := range invalid {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			in := valid()
			in.NotificationID = strPtr(tc.value)
			if _, err := NewRecord(in); err != ErrInvalidRecord {
				t.Fatalf("err = %v, want ErrInvalidRecord", err)
			}
		})
	}
}

func TestNewNotificationHistoryQuery(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t "} {
		if _, err := NewNotificationHistoryQuery(raw); err != ErrInvalidNotificationID {
			t.Fatalf("raw %q: err = %v, want ErrInvalidNotificationID", raw, err)
		}
	}
	for _, raw := range []string{"ntf-1", " ntf-1 "} {
		got, err := NewNotificationHistoryQuery(raw)
		if err != nil {
			t.Fatalf("raw %q: unexpected error %v", raw, err)
		}
		if got != raw {
			t.Fatalf("raw %q: got %q, want the value unchanged", raw, got)
		}
	}
}

func historyRecord(t *testing.T, occurredAt, status string, retryCount int, reason string) Record {
	t.Helper()
	record, err := NewRecord(RecordInput{
		TemplateID:    "tpl-1",
		Channel:       "sms",
		OccurredAt:    occurredAt,
		Status:        status,
		RetryCount:    intPtr(retryCount),
		FailureReason: reason,
	})
	if err != nil {
		t.Fatalf("new record: %v", err)
	}
	return record
}

func TestNewHistorySummaryEmpty(t *testing.T) {
	summary := NewHistorySummary(nil)
	if summary.TotalAttempts != 0 ||
		summary.StatusCounts != (StatusCounts{}) ||
		summary.RetryCountMin != nil || summary.RetryCountMax != nil ||
		summary.FirstAttemptAt != nil || summary.LastAttemptAt != nil {
		t.Fatalf("summary = %+v, want zeroed", summary)
	}
	if summary.FailureReasons == nil || len(summary.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %v, want an empty non-nil slice", summary.FailureReasons)
	}
}

func TestNewHistorySummaryAggregates(t *testing.T) {
	records := []Record{
		historyRecord(t, "2026-10-01T10:00:00Z", StatusPending, 0, ""),
		historyRecord(t, "2026-10-01T10:00:05Z", StatusRetrying, 1, "provider busy"),
		historyRecord(t, "2026-10-01T10:00:10Z", StatusFailed, 2, "provider timeout"),
		historyRecord(t, "2026-10-01T10:00:15Z", StatusFailed, 3, "provider timeout"),
		historyRecord(t, "2026-10-01T10:00:20Z", StatusSucceeded, 4, ""),
	}
	summary := NewHistorySummary(records)

	if summary.TotalAttempts != 5 {
		t.Fatalf("total attempts = %d, want 5", summary.TotalAttempts)
	}
	wantCounts := StatusCounts{Pending: 1, Retrying: 1, Succeeded: 1, Failed: 2}
	if summary.StatusCounts != wantCounts {
		t.Fatalf("status counts = %+v, want %+v", summary.StatusCounts, wantCounts)
	}
	if summary.RetryCountMin == nil || *summary.RetryCountMin != 0 {
		t.Fatalf("retry min = %v, want 0", summary.RetryCountMin)
	}
	if summary.RetryCountMax == nil || *summary.RetryCountMax != 4 {
		t.Fatalf("retry max = %v, want 4", summary.RetryCountMax)
	}
	if got := summary.FirstAttemptAt.Format(time.RFC3339); got != "2026-10-01T10:00:00Z" {
		t.Fatalf("first attempt = %s, want 2026-10-01T10:00:00Z", got)
	}
	if got := summary.LastAttemptAt.Format(time.RFC3339); got != "2026-10-01T10:00:20Z" {
		t.Fatalf("last attempt = %s, want 2026-10-01T10:00:20Z", got)
	}
	wantReasons := []FailureGroup{
		{FailureReason: "provider timeout", AttemptCount: 2},
		{FailureReason: "provider busy", AttemptCount: 1},
	}
	if !reflect.DeepEqual(summary.FailureReasons, wantReasons) {
		t.Fatalf("failure reasons = %+v, want %+v", summary.FailureReasons, wantReasons)
	}
}

func TestNewHistorySummaryOrdersReasonsByCountThenVerbatimText(t *testing.T) {
	records := []Record{
		historyRecord(t, "2026-10-01T10:00:00Z", StatusFailed, 1, "beta"),
		historyRecord(t, "2026-10-01T10:00:01Z", StatusFailed, 2, "alpha"),
		historyRecord(t, "2026-10-01T10:00:02Z", StatusRetrying, 3, "Beta"),
		historyRecord(t, "2026-10-01T10:00:03Z", StatusFailed, 4, "beta"),
	}
	summary := NewHistorySummary(records)
	want := []FailureGroup{
		{FailureReason: "beta", AttemptCount: 2},
		{FailureReason: "Beta", AttemptCount: 1},
		{FailureReason: "alpha", AttemptCount: 1},
	}
	if !reflect.DeepEqual(summary.FailureReasons, want) {
		t.Fatalf("failure reasons = %+v, want %+v", summary.FailureReasons, want)
	}
}
