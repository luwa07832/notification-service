package delivery

import "testing"

func validBatchInput(reason string) RecordInput {
	return RecordInput{
		TemplateID:    "tpl-1",
		Channel:       "sms",
		OccurredAt:    "2026-10-01T10:00:00Z",
		Status:        StatusFailed,
		RetryCount:    intPtr(1),
		FailureReason: reason,
	}
}

func TestNewRecordsAcceptsBatchAndKeepsOrder(t *testing.T) {
	first := validBatchInput("first failure")
	second := validBatchInput("second failure")
	second.Channel = "email"
	second.OccurredAt = "2026-10-01T18:00:00+08:00"

	records, err := NewRecords([]RecordInput{first, second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len = %d, want 2", len(records))
	}
	if records[0].Channel != "sms" || records[0].FailureReason != "first failure" {
		t.Fatalf("first record reordered or changed: %#v", records[0])
	}
	if records[1].Channel != "email" || records[1].FailureReason != "second failure" {
		t.Fatalf("second record reordered or changed: %#v", records[1])
	}
	if records[1].OccurredAt.UTC().Format("15:04:05") != "10:00:00" {
		t.Fatalf("second record not normalized to UTC: %v", records[1].OccurredAt)
	}
}

func TestNewRecordsRejectsOutOfRangeSizes(t *testing.T) {
	valid := validBatchInput("x")
	if _, err := NewRecords(nil); err != ErrInvalidBatch {
		t.Fatalf("nil batch err = %v, want ErrInvalidBatch", err)
	}
	if _, err := NewRecords([]RecordInput{valid}); err != ErrInvalidBatch {
		t.Fatalf("single element err = %v, want ErrInvalidBatch", err)
	}
	oversized := make([]RecordInput, MaxBatchRecords+1)
	for i := range oversized {
		oversized[i] = valid
	}
	if _, err := NewRecords(oversized); err != ErrInvalidBatch {
		t.Fatalf("oversized batch err = %v, want ErrInvalidBatch", err)
	}
}

func TestNewRecordsRejectsAnyInvalidElement(t *testing.T) {
	valid := validBatchInput("x")
	invalid := validBatchInput("x")
	invalid.Status = StatusSucceeded
	invalid.FailureReason = "should be empty"

	for _, inputs := range [][]RecordInput{
		{invalid, valid},
		{valid, invalid},
	} {
		records, err := NewRecords(inputs)
		if err != ErrInvalidBatch {
			t.Fatalf("err = %v, want ErrInvalidBatch", err)
		}
		if records != nil {
			t.Fatalf("invalid batch returned records: %#v", records)
		}
	}
}
