package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// DeliveryTrend buckets registered attempts into a continuous run of aligned
// UTC buckets for one channel and the half-open [start, end) range. Every
// bucket in the range is returned in bucket_start ascending order, including
// buckets with no attempts; no attempts or failure reasons are ever
// fabricated, and registration plus stored history stay untouched.
func (s *Store) DeliveryTrend(ctx context.Context, query delivery.TrendQuery) ([]delivery.TrendBucket, error) {
	bucketSize := time.Hour
	if query.Interval == delivery.IntervalDay {
		bucketSize = 24 * time.Hour
	}
	bucketCount := int(query.End.Sub(query.Start) / bucketSize)

	buckets := make([]delivery.TrendBucket, bucketCount)
	failureCounts := make([]map[string]int, bucketCount)
	for i := range buckets {
		bucketStart := query.Start.Add(time.Duration(i) * bucketSize)
		buckets[i] = delivery.TrendBucket{
			BucketStart:    bucketStart,
			BucketEnd:      bucketStart.Add(bucketSize),
			FailureReasons: []delivery.FailureGroup{},
		}
		failureCounts[i] = map[string]int{}
	}

	clauses := []string{
		"channel = ?",
		"occurred_at >= ?",
		"occurred_at < ?",
	}
	args := []any{
		query.Channel,
		query.Start.UTC().Format(storedTimeFormat),
		query.End.UTC().Format(storedTimeFormat),
	}
	if query.TemplateID != nil {
		clauses = append(clauses, "template_id = ?")
		args = append(args, *query.TemplateID)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT occurred_at, status, retry_count, failure_reason
		   FROM delivery_records
		  WHERE `+strings.Join(clauses, " AND "),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer rows.Close()

	for rows.Next() {
		var occurredAt, status, failureReason string
		var retryCount int
		if err := rows.Scan(&occurredAt, &status, &retryCount, &failureReason); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		parsedAt, err := time.Parse(storedTimeFormat, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}

		index := int(parsedAt.UTC().Sub(query.Start) / bucketSize)
		if index < 0 || index >= bucketCount {
			return nil, fmt.Errorf("%w: record outside validated trend range", ErrStorageUnavailable)
		}
		bucket := &buckets[index]
		bucket.TotalAttempts++
		if bucket.RetryCountMin == nil || retryCount < *bucket.RetryCountMin {
			value := retryCount
			bucket.RetryCountMin = &value
		}
		if bucket.RetryCountMax == nil || retryCount > *bucket.RetryCountMax {
			value := retryCount
			bucket.RetryCountMax = &value
		}
		switch status {
		case delivery.StatusPending:
			bucket.StatusCounts.Pending++
		case delivery.StatusRetrying:
			bucket.StatusCounts.Retrying++
		case delivery.StatusSucceeded:
			bucket.StatusCounts.Succeeded++
		case delivery.StatusFailed:
			bucket.StatusCounts.Failed++
		}
		if (status == delivery.StatusFailed || status == delivery.StatusRetrying) && failureReason != "" {
			failureCounts[index][failureReason]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	for i := range buckets {
		counts := failureCounts[i]
		reasons := make([]delivery.FailureGroup, 0, len(counts))
		for reason, count := range counts {
			reasons = append(reasons, delivery.FailureGroup{FailureReason: reason, AttemptCount: count})
		}
		sort.Slice(reasons, func(j, k int) bool {
			if reasons[j].AttemptCount != reasons[k].AttemptCount {
				return reasons[j].AttemptCount > reasons[k].AttemptCount
			}
			return reasons[j].FailureReason < reasons[k].FailureReason
		})
		buckets[i].FailureReasons = reasons
	}
	return buckets, nil
}
