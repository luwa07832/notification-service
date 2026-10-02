package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// DeliveryTrend aggregates registered attempts into the consecutive UTC
// buckets the validated query describes, ordered by bucket start ascending.
// Buckets without any attempt still appear with zero counts and nil retry
// bounds. The scan only reads existing rows: registration, retry handling
// and stored failure reasons stay untouched, and no missing attempts or
// failure reasons are ever fabricated.
func (s *Store) DeliveryTrend(ctx context.Context, query delivery.TrendQuery) ([]delivery.TrendBucket, error) {
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

	width := query.BucketWidth()
	bucketCount := int(query.End.Sub(query.Start) / width)
	type bucketAggregate struct {
		total        int
		statusCounts delivery.StatusCounts
		retryMin     int
		retryMax     int
		reasons      map[string]int
	}
	aggregates := make([]bucketAggregate, bucketCount)
	for i := range aggregates {
		aggregates[i].reasons = map[string]int{}
	}

	for rows.Next() {
		var (
			occurredAtRaw, status, failureReason string
			retryCount                           int
		)
		if err := rows.Scan(&occurredAtRaw, &status, &retryCount, &failureReason); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		occurredAt, err := time.Parse(storedTimeFormat, occurredAtRaw)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
		}
		index := int(occurredAt.UTC().Sub(query.Start) / width)
		if index < 0 || index >= bucketCount {
			continue
		}
		bucket := &aggregates[index]
		bucket.total++
		if bucket.total == 1 || retryCount < bucket.retryMin {
			bucket.retryMin = retryCount
		}
		if retryCount > bucket.retryMax {
			bucket.retryMax = retryCount
		}
		switch status {
		case delivery.StatusPending:
			bucket.statusCounts.Pending++
		case delivery.StatusRetrying:
			bucket.statusCounts.Retrying++
		case delivery.StatusSucceeded:
			bucket.statusCounts.Succeeded++
		case delivery.StatusFailed:
			bucket.statusCounts.Failed++
		}
		if (status == delivery.StatusFailed || status == delivery.StatusRetrying) && failureReason != "" {
			bucket.reasons[failureReason]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	buckets := make([]delivery.TrendBucket, bucketCount)
	for i := range buckets {
		bucketStart := query.Start.Add(time.Duration(i) * width)
		aggregate := aggregates[i]
		bucket := delivery.TrendBucket{
			BucketStart:    bucketStart,
			BucketEnd:      bucketStart.Add(width),
			TotalAttempts:  aggregate.total,
			StatusCounts:   aggregate.statusCounts,
			FailureReasons: []delivery.FailureGroup{},
		}
		if aggregate.total > 0 {
			retryMin := aggregate.retryMin
			retryMax := aggregate.retryMax
			bucket.RetryCountMin = &retryMin
			bucket.RetryCountMax = &retryMax
		}
		reasons := make([]delivery.FailureGroup, 0, len(aggregate.reasons))
		for reason, count := range aggregate.reasons {
			reasons = append(reasons, delivery.FailureGroup{FailureReason: reason, AttemptCount: count})
		}
		sort.Slice(reasons, func(i, j int) bool {
			if reasons[i].AttemptCount != reasons[j].AttemptCount {
				return reasons[i].AttemptCount > reasons[j].AttemptCount
			}
			return reasons[i].FailureReason < reasons[j].FailureReason
		})
		bucket.FailureReasons = reasons
		buckets[i] = bucket
	}
	return buckets, nil
}
