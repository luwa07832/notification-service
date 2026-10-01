package delivery

import (
	"testing"
)

func ptrInt(v int) *int          { return &v }
func ptrString(v string) *string { return &v }

func TestValidateWriteAcceptsEveryStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string
		reason *string
	}{
		{"pending without reason", StatusPending, nil},
		{"retrying with reason", StatusRetrying, ptrString("provider timeout")},
		{"succeeded without reason", StatusSucceeded, nil},
		{"failed with reason", StatusFailed, ptrString("bounced")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{
				TemplateID:    "tpl-1",
				Channel:       "email",
				OccurredAt:    "2026-10-01T08:00:00Z",
				Status:        tc.status,
				RetryCount:    ptrInt(0),
				FailureReason: tc.reason,
			}
			if _, _, err := ValidateWrite(in); err != nil {
				t.Fatalf("ValidateWrite: %v", err)
			}
		})
	}
}

func TestValidateWriteRejectsBadInput(t *testing.T) {
	base := func() Input {
		return Input{
			TemplateID:    "tpl-1",
			Channel:       "sms",
			OccurredAt:    "2026-10-01T08:00:00Z",
			Status:        StatusFailed,
			RetryCount:    ptrInt(1),
			FailureReason: ptrString("provider error"),
		}
	}

	cases := []struct {
		name   string
		mutate func(*Input)
	}{
		{"missing template", func(in *Input) { in.TemplateID = "  " }},
		{"unknown channel", func(in *Input) { in.Channel = "carrier-pigeon" }},
		{"bad timestamp", func(in *Input) { in.OccurredAt = "yesterday" }},
		{"bad status", func(in *Input) { in.Status = "done" }},
		{"missing retry count", func(in *Input) { in.RetryCount = nil }},
		{"negative retry count", func(in *Input) { in.RetryCount = ptrInt(-1) }},
		{"failed without reason", func(in *Input) { in.FailureReason = nil }},
		{"retrying with blank reason", func(in *Input) {
			in.Status = StatusRetrying
			in.FailureReason = ptrString("   ")
		}},
		{"succeeded with reason", func(in *Input) {
			in.Status = StatusSucceeded
		}},
		{"pending with reason", func(in *Input) {
			in.Status = StatusPending
			in.FailureReason = ptrString("should not be here")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mutate(&in)
			if _, _, err := ValidateWrite(in); err == nil {
				t.Fatalf("ValidateWrite accepted %s", tc.name)
			}
		})
	}
}

func TestValidateQuery(t *testing.T) {
	if _, _, err := ValidateQuery("email", "2026-10-01T08:00:00Z", "2026-10-01T09:00:00Z"); err != nil {
		t.Fatalf("valid query rejected: %v", err)
	}
	bad := []struct {
		channel, start, end string
	}{
		{"fax", "2026-10-01T08:00:00Z", "2026-10-01T09:00:00Z"},
		{"email", "noon", "2026-10-01T09:00:00Z"},
		{"email", "2026-10-01T08:00:00Z", "later"},
		{"email", "2026-10-01T09:00:00Z", "2026-10-01T08:00:00Z"},
		{"email", "2026-10-01T08:00:00Z", "2026-10-01T08:00:00Z"},
	}
	for _, tc := range bad {
		if _, _, err := ValidateQuery(tc.channel, tc.start, tc.end); err == nil {
			t.Fatalf("ValidateQuery accepted channel=%q start=%q end=%q", tc.channel, tc.start, tc.end)
		}
	}
}

func TestCanonicalTimeOrdersChronologically(t *testing.T) {
	earlier, _ := ParseTime("2026-10-01T08:00:00+02:00")
	later, _ := ParseTime("2026-10-01T07:00:00.5Z")
	sameInstant, _ := ParseTime("2026-10-01T06:00:00Z")
	if CanonicalTime(earlier) != CanonicalTime(sameInstant) {
		t.Fatalf("same instant normalized differently: %q vs %q", CanonicalTime(earlier), CanonicalTime(sameInstant))
	}
	if !(CanonicalTime(earlier) < CanonicalTime(later)) {
		t.Fatalf("canonical order wrong: %q should precede %q", CanonicalTime(earlier), CanonicalTime(later))
	}
}
