package store

import (
	"context"
	"testing"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

func seedTrendRecords(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:30:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "timeout"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:45:00Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:59:59Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T12:00:00Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(3), FailureReason: "bounce"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T12:30:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "bounce"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T13:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(9), FailureReason: "at end boundary"},
		{TemplateID: "tpl-9", Channel: "sms", OccurredAt: "2026-10-01T09:59:59Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "before start"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:00:05Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(1), FailureReason: "Timeout"},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
}

func mustTrend(t *testing.T, mutate func(*delivery.TrendInput)) delivery.TrendQuery {
	t.Helper()
	in := delivery.TrendInput{
		Channel:  "sms",
		Start:    "2026-10-01T10:00:00Z",
		End:      "2026-10-01T13:00:00Z",
		Interval: delivery.IntervalHour,
	}
	mutate(&in)
	query, err := delivery.NewTrend(in)
	if err != nil {
		t.Fatalf("new trend: %v", err)
	}
	return query
}

func TestDeliveryTrendCoversConsecutiveHourBuckets(t *testing.T) {
	st := openTestStore(t)
	seedTrendRecords(t, st)

	buckets, err := st.DeliveryTrend(context.Background(), mustTrend(t, func(in *delivery.TrendInput) {}))
	if err != nil {
		t.Fatalf("delivery trend: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("buckets = %+v, want 3", buckets)
	}

	first := buckets[0]
	if !first.BucketStart.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) ||
		!first.BucketEnd.Equal(time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("first bounds = %v..%v", first.BucketStart, first.BucketEnd)
	}
	if first.TotalAttempts != 5 {
		t.Fatalf("first total = %d, want 5", first.TotalAttempts)
	}
	wantCounts := delivery.StatusCounts{Pending: 1, Retrying: 1, Succeeded: 1, Failed: 2}
	if first.StatusCounts != wantCounts {
		t.Fatalf("first counts = %+v, want %+v", first.StatusCounts, wantCounts)
	}
	if first.RetryCountMin == nil || *first.RetryCountMin != 0 ||
		first.RetryCountMax == nil || *first.RetryCountMax != 2 {
		t.Fatalf("first retry bounds = %v..%v", first.RetryCountMin, first.RetryCountMax)
	}
	wantReasons := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "timeout", AttemptCount: 1},
	}
	if len(first.FailureReasons) != len(wantReasons) {
		t.Fatalf("first reasons = %+v", first.FailureReasons)
	}
	for i := range wantReasons {
		if first.FailureReasons[i] != wantReasons[i] {
			t.Fatalf("first reason %d = %+v, want %+v", i, first.FailureReasons[i], wantReasons[i])
		}
	}

	empty := buckets[1]
	if !empty.BucketStart.Equal(time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)) ||
		!empty.BucketEnd.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("empty bounds = %v..%v", empty.BucketStart, empty.BucketEnd)
	}
	if empty.TotalAttempts != 0 || empty.StatusCounts != (delivery.StatusCounts{}) {
		t.Fatalf("empty bucket = %+v, want zeros", empty)
	}
	if empty.RetryCountMin != nil || empty.RetryCountMax != nil {
		t.Fatalf("empty retry bounds = %v..%v, want nil", empty.RetryCountMin, empty.RetryCountMax)
	}
	if len(empty.FailureReasons) != 0 {
		t.Fatalf("empty reasons = %+v, want none", empty.FailureReasons)
	}

	last := buckets[2]
	if last.TotalAttempts != 2 {
		t.Fatalf("last total = %d, want 2", last.TotalAttempts)
	}
	if last.StatusCounts != (delivery.StatusCounts{Retrying: 1, Failed: 1}) {
		t.Fatalf("last counts = %+v", last.StatusCounts)
	}
	if last.RetryCountMin == nil || *last.RetryCountMin != 1 ||
		last.RetryCountMax == nil || *last.RetryCountMax != 3 {
		t.Fatalf("last retry bounds = %v..%v", last.RetryCountMin, last.RetryCountMax)
	}
	if len(last.FailureReasons) != 1 || last.FailureReasons[0] != (delivery.FailureGroup{FailureReason: "bounce", AttemptCount: 2}) {
		t.Fatalf("last reasons = %+v", last.FailureReasons)
	}
}

func TestDeliveryTrendDayInterval(t *testing.T) {
	st := openTestStore(t)
	seedTrendRecords(t, st)

	buckets, err := st.DeliveryTrend(context.Background(), mustTrend(t, func(in *delivery.TrendInput) {
		in.Interval = delivery.IntervalDay
		in.Start = "2026-10-01T00:00:00Z"
		in.End = "2026-10-03T00:00:00Z"
	}))
	if err != nil {
		t.Fatalf("delivery trend: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("buckets = %+v, want 2", buckets)
	}
	day := buckets[0]
	if !day.BucketStart.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) ||
		!day.BucketEnd.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day bounds = %v..%v", day.BucketStart, day.BucketEnd)
	}
	if day.TotalAttempts != 9 {
		t.Fatalf("day total = %d, want 9", day.TotalAttempts)
	}
	if day.RetryCountMin == nil || *day.RetryCountMin != 0 ||
		day.RetryCountMax == nil || *day.RetryCountMax != 9 {
		t.Fatalf("day retry bounds = %v..%v", day.RetryCountMin, day.RetryCountMax)
	}
	wantReasons := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "bounce", AttemptCount: 2},
		{FailureReason: "at end boundary", AttemptCount: 1},
		{FailureReason: "before start", AttemptCount: 1},
		{FailureReason: "timeout", AttemptCount: 1},
	}
	if len(day.FailureReasons) != len(wantReasons) {
		t.Fatalf("day reasons = %+v", day.FailureReasons)
	}
	for i := range wantReasons {
		if day.FailureReasons[i] != wantReasons[i] {
			t.Fatalf("day reason %d = %+v, want %+v", i, day.FailureReasons[i], wantReasons[i])
		}
	}
	if buckets[1].TotalAttempts != 0 || buckets[1].RetryCountMin != nil {
		t.Fatalf("second day = %+v, want empty", buckets[1])
	}
}

func TestDeliveryTrendTemplateFilter(t *testing.T) {
	st := openTestStore(t)
	seedTrendRecords(t, st)

	buckets, err := st.DeliveryTrend(context.Background(), mustTrend(t, func(in *delivery.TrendInput) {
		templateID := "tpl-1"
		in.TemplateID = &templateID
	}))
	if err != nil {
		t.Fatalf("delivery trend: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("buckets = %+v, want 3", buckets)
	}
	if buckets[0].TotalAttempts != 4 || buckets[1].TotalAttempts != 0 || buckets[2].TotalAttempts != 1 {
		t.Fatalf("totals = %d/%d/%d", buckets[0].TotalAttempts, buckets[1].TotalAttempts, buckets[2].TotalAttempts)
	}
	if len(buckets[0].FailureReasons) != 2 || buckets[0].FailureReasons[0].FailureReason != "Timeout" {
		t.Fatalf("first reasons = %+v", buckets[0].FailureReasons)
	}
}

func TestDeliveryTrendEmptyRangeKeepsBuckets(t *testing.T) {
	st := openTestStore(t)
	seedTrendRecords(t, st)

	buckets, err := st.DeliveryTrend(context.Background(), mustTrend(t, func(in *delivery.TrendInput) {
		in.Channel = "push"
	}))
	if err != nil {
		t.Fatalf("delivery trend: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("buckets = %+v, want 3", buckets)
	}
	for i, bucket := range buckets {
		if bucket.TotalAttempts != 0 || bucket.RetryCountMin != nil || bucket.RetryCountMax != nil {
			t.Fatalf("bucket %d = %+v, want empty", i, bucket)
		}
		if len(bucket.FailureReasons) != 0 {
			t.Fatalf("bucket %d reasons = %+v, want none", i, bucket.FailureReasons)
		}
	}
}
