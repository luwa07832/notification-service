package delivery

import (
	"errors"
	"testing"
	"time"
)

func notifID(v string) *string { return &v }

func TestNewRecordNotificationID(t *testing.T) {
	base := RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
		Status: StatusSucceeded, RetryCount: ptrTestInt(0),
	}

	record, err := NewRecord(base)
	if err != nil {
		t.Fatalf("omitted notification id: %v", err)
	}
	if record.NotificationID != "" {
		t.Fatalf("omitted notification id = %q, want empty", record.NotificationID)
	}

	cases := []string{"n-1", "notif-with-dash", "n 1"}
	for _, value := range cases {
		in := base
		in.NotificationID = notifID(value)
		record, err := NewRecord(in)
		if err != nil {
			t.Fatalf("notification id %q: %v", value, err)
		}
		if record.NotificationID != value {
			t.Fatalf("notification id = %q, want %q", record.NotificationID, value)
		}
	}

	for _, value := range []string{"", " ", "\t", " n", "n ", "\nn"} {
		in := base
		in.NotificationID = notifID(value)
		if _, err := NewRecord(in); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("notification id %q: err = %v, want ErrInvalidRecord", value, err)
		}
	}
}

func ptrTestInt(v int) *int { return &v }

func TestNewRecordsNotificationIDBatchRejectsInvalidElement(t *testing.T) {
	valid := RecordInput{
		TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
		Status: StatusFailed, RetryCount: ptrTestInt(0), FailureReason: "x",
		NotificationID: notifID("n-1"),
	}
	invalid := valid
	invalid.NotificationID = notifID(" n-1 ")

	if _, err := NewRecords([]RecordInput{valid, invalid}); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("batch err = %v, want ErrInvalidBatch", err)
	}

	records, err := NewRecords([]RecordInput{valid, valid})
	if err != nil {
		t.Fatalf("same notification id in two elements: %v", err)
	}
	if records[0].NotificationID != "n-1" || records[1].NotificationID != "n-1" {
		t.Fatalf("notification ids = %q, %q", records[0].NotificationID, records[1].NotificationID)
	}
}

func TestNewNotificationIDValidation(t *testing.T) {
	if _, err := NewNotificationID(""); !errors.Is(err, ErrInvalidNotificationID) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := NewNotificationID("   "); !errors.Is(err, ErrInvalidNotificationID) {
		t.Fatalf("blank: %v", err)
	}
	id, err := NewNotificationID("  n-1  ")
	if err != nil {
		t.Fatalf("valid: %v", err)
	}
	if id != "n-1" {
		t.Fatalf("trimmed id = %q", id)
	}
}

func mustHistoryRecord(t *testing.T, notificationID, at, status string, retry int, reason string) Record {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatalf("parse %s: %v", at, err)
	}
	return Record{
		ID:             "id-" + at[11:] + "-" + status,
		TemplateID:     "tpl-1",
		Channel:        "sms",
		OccurredAt:     parsed.UTC(),
		Status:         status,
		RetryCount:     retry,
		FailureReason:  reason,
		NotificationID: notificationID,
	}
}

func TestBuildHistoryAggregatesRetryChain(t *testing.T) {
	records := []Record{
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:00Z", StatusPending, 0, ""),
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:05Z", StatusRetrying, 1, "busy"),
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:05Z", StatusRetrying, 1, "busy"),
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:10Z", StatusFailed, 2, "timeout"),
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:15Z", StatusRetrying, 3, "busy"),
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:20Z", StatusSucceeded, 4, ""),
	}
	history := BuildHistory(records)

	if history.Summary.TotalAttempts != 6 {
		t.Fatalf("total = %d", history.Summary.TotalAttempts)
	}
	counts := history.Summary.StatusCounts
	if counts.Pending != 1 || counts.Retrying != 3 || counts.Succeeded != 1 || counts.Failed != 1 {
		t.Fatalf("status counts = %+v", counts)
	}
	if *history.Summary.RetryCountMin != 0 || *history.Summary.RetryCountMax != 4 {
		t.Fatalf("retry bounds = %v..%v", *history.Summary.RetryCountMin, *history.Summary.RetryCountMax)
	}
	if history.Summary.FirstAttemptAt.Format(time.RFC3339) != "2026-10-01T10:00:00Z" {
		t.Fatalf("first = %v", history.Summary.FirstAttemptAt)
	}
	if history.Summary.LastAttemptAt.Format(time.RFC3339) != "2026-10-01T10:00:20Z" {
		t.Fatalf("last = %v", history.Summary.LastAttemptAt)
	}
	reasons := history.Summary.FailureReasons
	if len(reasons) != 2 {
		t.Fatalf("failure reasons = %+v", reasons)
	}
	if reasons[0].FailureReason != "busy" || reasons[0].AttemptCount != 3 {
		t.Fatalf("first group = %+v, want busy x3", reasons[0])
	}
	if reasons[1].FailureReason != "timeout" || reasons[1].AttemptCount != 1 {
		t.Fatalf("second group = %+v, want timeout x1", reasons[1])
	}
}

func TestBuildHistoryFailureReasonsTieBreakByText(t *testing.T) {
	records := []Record{
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:00Z", StatusFailed, 1, "b reason"),
		mustHistoryRecord(t, "n-1", "2026-10-01T10:00:05Z", StatusFailed, 2, "a reason"),
	}
	history := BuildHistory(records)
	reasons := history.Summary.FailureReasons
	if reasons[0].FailureReason != "a reason" || reasons[1].FailureReason != "b reason" {
		t.Fatalf("tie order = %+v", reasons)
	}
}

func TestBuildHistoryEmpty(t *testing.T) {
	history := BuildHistory(nil)
	if history.Summary.TotalAttempts != 0 {
		t.Fatalf("total = %d", history.Summary.TotalAttempts)
	}
	counts := history.Summary.StatusCounts
	if counts.Pending != 0 || counts.Retrying != 0 || counts.Succeeded != 0 || counts.Failed != 0 {
		t.Fatalf("counts = %+v", counts)
	}
	if history.Summary.RetryCountMin != nil || history.Summary.RetryCountMax != nil ||
		history.Summary.FirstAttemptAt != nil || history.Summary.LastAttemptAt != nil {
		t.Fatalf("bounds must be nil: %+v", history.Summary)
	}
	if len(history.Summary.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %+v", history.Summary.FailureReasons)
	}
}
