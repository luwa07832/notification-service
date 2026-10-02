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
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:15:00Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "Timeout"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:30:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(3), FailureReason: "bounce"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:45:00Z",
			Status: delivery.StatusPending, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-01T10:59:59Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T12:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(5), FailureReason: "at end bucket start"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T09:59:59Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(9), FailureReason: "before start"},
		{TemplateID: "tpl-1", Channel: "email", OccurredAt: "2026-10-01T10:10:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(8), FailureReason: "other channel"},
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

func findTrendBucket(t *testing.T, buckets []delivery.TrendBucket, want string) delivery.TrendBucket {
	t.Helper()
	wantAt, err := time.Parse(time.RFC3339, want)
	if err != nil {
		t.Fatalf("parse %s: %v", want, err)
	}
	for _, bucket := range buckets {
		if bucket.BucketStart.Equal(wantAt) {
			return bucket
		}
	}
	t.Fatalf("bucket starting %s not found", want)
	return delivery.TrendBucket{}
}

func TestDeliveryTrendBuildsContinuousHourBuckets(t *testing.T) {
	st := openTestStore(t)
	seedTrendRecords(t, st)

	buckets, err := st.DeliveryTrend(context.Background(), mustTrend(t, func(*delivery.TrendInput) {}))
	if err != nil {
		t.Fatalf("trend: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("bucket count = %d, want 3", len(buckets))
	}

	wantStarts := []string{
		"2026-10-01T10:00:00Z",
		"2026-10-01T11:00:00Z",
		"2026-10-01T12:00:00Z",
	}
	for i, want := range wantStarts {
		wantStart, _ := time.Parse(time.RFC3339, want)
		if !buckets[i].BucketStart.Equal(wantStart) {
			t.Fatalf("bucket %d start = %v, want %v", i, buckets[i].BucketStart, wantStart)
		}
		if !buckets[i].BucketEnd.Equal(wantStart.Add(time.Hour)) {
			t.Fatalf("bucket %d end = %v", i, buckets[i].BucketEnd)
		}
	}

	first := buckets[0]
	if first.TotalAttempts != 5 {
		t.Fatalf("total attempts = %d, want 5", first.TotalAttempts)
	}
	if first.StatusCounts != (delivery.StatusCounts{Pending: 1, Retrying: 1, Succeeded: 1, Failed: 2}) {
		t.Fatalf("status counts = %+v", first.StatusCounts)
	}
	if first.RetryCountMin == nil || *first.RetryCountMin != 0 {
		t.Fatalf("retry min = %v, want 0", first.RetryCountMin)
	}
	if first.RetryCountMax == nil || *first.RetryCountMax != 3 {
		t.Fatalf("retry max = %v, want 3", first.RetryCountMax)
	}
	wantReasons := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "bounce", AttemptCount: 1},
	}
	if len(first.FailureReasons) != len(wantReasons) {
		t.Fatalf("failure reasons = %+v", first.FailureReasons)
	}
	for i, want := range wantReasons {
		if first.FailureReasons[i] != want {
			t.Fatalf("reason %d = %+v, want %+v", i, first.FailureReasons[i], want)
		}
	}

	empty := buckets[1]
	if empty.TotalAttempts != 0 || empty.StatusCounts != (delivery.StatusCounts{}) {
		t.Fatalf("empty bucket = %+v", empty)
	}
	if empty.RetryCountMin != nil || empty.RetryCountMax != nil {
		t.Fatalf("empty bucket retry extrema = %v/%p", empty.RetryCountMin, empty.RetryCountMax)
	}
	if len(empty.FailureReasons) != 0 {
		t.Fatalf("empty bucket failure reasons = %+v", empty.FailureReasons)
	}

	last := findTrendBucket(t, buckets, "2026-10-01T12:00:00Z")
	if last.TotalAttempts != 1 || last.StatusCounts.Failed != 1 {
		t.Fatalf("last bucket = %+v", last)
	}
	if *last.RetryCountMin != 5 || *last.RetryCountMax != 5 {
		t.Fatalf("last bucket retry extrema = %v/%v", last.RetryCountMin, last.RetryCountMax)
	}
}

func TestDeliveryTrendUsesDayBucketsAndTemplateFilter(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	dayInputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T23:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(2), FailureReason: "Timeout"},
		{TemplateID: "tpl-2", Channel: "sms", OccurredAt: "2026-10-02T01:00:00Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(4)},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-02T12:00:00Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(6), FailureReason: "Timeout"},
	}
	for i, in := range dayInputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	query := mustTrend(t, func(in *delivery.TrendInput) {
		in.Start = "2026-10-01T00:00:00Z"
		in.End = "2026-10-03T00:00:00Z"
		in.Interval = delivery.IntervalDay
		templateID := "tpl-1"
		in.TemplateID = &templateID
	})
	buckets, err := st.DeliveryTrend(ctx, query)
	if err != nil {
		t.Fatalf("trend: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("bucket count = %d, want 2", len(buckets))
	}
	if !buckets[0].BucketEnd.Equal(buckets[1].BucketStart) {
		t.Fatalf("buckets are not continuous: %+v", buckets)
	}
	dayOne := findTrendBucket(t, buckets, "2026-10-01T00:00:00Z")
	if dayOne.TotalAttempts != 1 || dayOne.StatusCounts.Failed != 1 {
		t.Fatalf("day one = %+v", dayOne)
	}
	dayTwo := findTrendBucket(t, buckets, "2026-10-02T00:00:00Z")
	if dayTwo.TotalAttempts != 1 || dayTwo.StatusCounts.Retrying != 1 {
		t.Fatalf("day two = %+v", dayTwo)
	}
	if *dayTwo.RetryCountMin != 6 || *dayTwo.RetryCountMax != 6 {
		t.Fatalf("day two retry extrema = %v/%v", dayTwo.RetryCountMin, dayTwo.RetryCountMax)
	}
}

func TestDeliveryTrendReturnsAllZeroBucketsWithoutRows(t *testing.T) {
	st := openTestStore(t)
	buckets, err := st.DeliveryTrend(context.Background(), mustTrend(t, func(*delivery.TrendInput) {}))
	if err != nil {
		t.Fatalf("trend: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("bucket count = %d, want 3", len(buckets))
	}
	for i, bucket := range buckets {
		if bucket.TotalAttempts != 0 || bucket.StatusCounts != (delivery.StatusCounts{}) {
			t.Fatalf("bucket %d not zero: %+v", i, bucket)
		}
		if bucket.RetryCountMin != nil || bucket.RetryCountMax != nil {
			t.Fatalf("bucket %d retry extrema not null", i)
		}
		if len(bucket.FailureReasons) != 0 {
			t.Fatalf("bucket %d failure reasons = %+v", i, bucket.FailureReasons)
		}
	}
}

func TestDeliveryTrendOrdersFailureReasonsByCountThenText(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	inputs := []delivery.RecordInput{
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:00:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:05:00Z",
			Status: delivery.StatusFailed, RetryCount: ptrInt(0), FailureReason: "Timeout"},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:10:00Z",
			Status: delivery.StatusRetrying, RetryCount: ptrInt(1), FailureReason: "Timeout "},
		{TemplateID: "tpl-1", Channel: "sms", OccurredAt: "2026-10-01T10:15:00Z",
			Status: delivery.StatusSucceeded, RetryCount: ptrInt(0)},
	}
	for i, in := range inputs {
		if _, err := st.CreateDeliveryRecord(ctx, mustRecord(t, in)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	buckets, err := st.DeliveryTrend(ctx, mustTrend(t, func(in *delivery.TrendInput) {
		in.End = "2026-10-01T11:00:00Z"
	}))
	if err != nil {
		t.Fatalf("trend: %v", err)
	}
	reasons := buckets[0].FailureReasons
	want := []delivery.FailureGroup{
		{FailureReason: "Timeout", AttemptCount: 1},
		{FailureReason: "Timeout ", AttemptCount: 1},
		{FailureReason: "timeout", AttemptCount: 1},
	}
	if len(reasons) != len(want) {
		t.Fatalf("reasons = %+v", reasons)
	}
	for i := range want {
		if reasons[i] != want[i] {
			t.Fatalf("reason %d = %+v, want %+v", i, reasons[i], want[i])
		}
	}
}
