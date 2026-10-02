package delivery

import "testing"

func intPtr(v int) *int { return &v }

func TestNewRecordAcceptsValidStatusReasonCombinations(t *testing.T) {
	cases := []struct {
		name   string
		status string
		reason string
	}{
		{"pending empty", StatusPending, ""},
		{"succeeded empty", StatusSucceeded, ""},
		{"retrying has reason", StatusRetrying, "provider busy"},
		{"failed has reason", StatusFailed, "recipient bounced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRecord(RecordInput{
				TemplateID:    "tpl-1",
				Channel:       "sms",
				OccurredAt:    "2026-10-01T10:00:00Z",
				Status:        tc.status,
				RetryCount:    intPtr(2),
				FailureReason: tc.reason,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewRecordRejectsInvalidInputs(t *testing.T) {
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
	cases := []struct {
		name   string
		mutate func(*RecordInput)
	}{
		{"empty template id", func(in *RecordInput) { in.TemplateID = "  " }},
		{"unknown channel", func(in *RecordInput) { in.Channel = "carrier-pigeon" }},
		{"whitespace channel", func(in *RecordInput) { in.Channel = " sms" }},
		{"bad time", func(in *RecordInput) { in.OccurredAt = "yesterday" }},
		{"unknown status", func(in *RecordInput) { in.Status = "done" }},
		{"missing retry count", func(in *RecordInput) { in.RetryCount = nil }},
		{"negative retry count", func(in *RecordInput) { in.RetryCount = intPtr(-1) }},
		{"failed missing reason", func(in *RecordInput) { in.Status = StatusFailed; in.FailureReason = "" }},
		{"retrying blank reason", func(in *RecordInput) { in.Status = StatusRetrying; in.FailureReason = "   " }},
		{"pending with reason", func(in *RecordInput) { in.Status = StatusPending; in.FailureReason = "why" }},
		{"succeeded with reason", func(in *RecordInput) { in.Status = StatusSucceeded; in.FailureReason = "why" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := valid()
			tc.mutate(&in)
			if _, err := NewRecord(in); err != ErrInvalidRecord {
				t.Fatalf("err = %v, want ErrInvalidRecord", err)
			}
		})
	}
}

func TestNewRecordNormalizesOccurredAtToUTC(t *testing.T) {
	record, err := NewRecord(RecordInput{
		TemplateID: "tpl-1", Channel: "email",
		OccurredAt: "2026-10-01T18:00:00+08:00",
		Status:     StatusPending, RetryCount: intPtr(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := record.OccurredAt.Format("2006-01-02T15:04:05Z07:00"), "2026-10-01T10:00:00Z"; got != want {
		t.Fatalf("occurred at = %s, want %s", got, want)
	}
}

func TestNewQueryRejectsInvalidInputs(t *testing.T) {
	if _, err := NewQuery("sms", "2026-10-01T10:00:00Z", "2026-10-01T11:00:00Z"); err != nil {
		t.Fatalf("valid query rejected: %v", err)
	}
	invalid := []struct {
		name                string
		channel, start, end string
	}{
		{"unknown channel", "fax", "2026-10-01T10:00:00Z", "2026-10-01T11:00:00Z"},
		{"bad start", "sms", "soon", "2026-10-01T11:00:00Z"},
		{"bad end", "sms", "2026-10-01T10:00:00Z", "late"},
		{"start equals end", "sms", "2026-10-01T10:00:00Z", "2026-10-01T10:00:00Z"},
		{"start after end", "sms", "2026-10-01T11:00:00Z", "2026-10-01T10:00:00Z"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewQuery(tc.channel, tc.start, tc.end); err != ErrInvalidQuery {
				t.Fatalf("err = %v, want ErrInvalidQuery", err)
			}
		})
	}
}

func batchInput(size int) BatchInput {
	records := make([]RecordInput, size)
	for i := range records {
		records[i] = RecordInput{
			TemplateID:    "tpl-1",
			Channel:       "sms",
			OccurredAt:    "2026-10-01T10:00:00Z",
			Status:        StatusPending,
			RetryCount:    intPtr(0),
			FailureReason: "",
		}
	}
	return BatchInput{Records: records}
}

func TestNewBatchAcceptsTwoToHundred(t *testing.T) {
	for _, size := range []int{MinBatchSize, 50, MaxBatchSize} {
		records, err := NewBatch(batchInput(size))
		if err != nil {
			t.Fatalf("size %d: unexpected error: %v", size, err)
		}
		if len(records) != size {
			t.Fatalf("size %d: got %d records", size, len(records))
		}
	}
}

func TestNewBatchRejectsBadSizes(t *testing.T) {
	for _, size := range []int{0, 1, MaxBatchSize + 1} {
		if _, err := NewBatch(batchInput(size)); err != ErrInvalidBatch {
			t.Fatalf("size %d: err = %v, want ErrInvalidBatch", size, err)
		}
	}
	var in BatchInput
	if _, err := NewBatch(in); err != ErrInvalidBatch {
		t.Fatalf("nil records: err = %v, want ErrInvalidBatch", err)
	}
}

func TestNewBatchRejectsAnyInvalidElementAndKeepsOrder(t *testing.T) {
	in := batchInput(3)
	in.Records[1].Status = "bogus"
	if _, err := NewBatch(in); err != ErrInvalidBatch {
		t.Fatalf("err = %v, want ErrInvalidBatch", err)
	}

	in = batchInput(3)
	in.Records[0].OccurredAt = "2026-10-01T12:00:00Z"
	in.Records[2].OccurredAt = "2026-10-01T11:00:00Z"
	records, err := NewBatch(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if records[0].OccurredAt.Hour() != 12 || records[2].OccurredAt.Hour() != 11 {
		t.Fatal("request order not preserved")
	}
}
