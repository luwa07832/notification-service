package delivery

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// ErrInvalidNotificationID marks a blank notification instance identifier.
// The HTTP layer maps it to the published INVALID_NOTIFICATION_ID error code.
var ErrInvalidNotificationID = errors.New("invalid notification id")

// HistorySummary aggregates the registered attempts of one notification
// instance. RetryCountMin, RetryCountMax, FirstAttemptAt and LastAttemptAt are
// nil when the instance has no attempts; status counts always cover the four
// published statuses.
type HistorySummary struct {
	TotalAttempts  int            `json:"total_attempts"`
	StatusCounts   StatusCounts   `json:"status_counts"`
	RetryCountMin  *int           `json:"retry_count_min"`
	RetryCountMax  *int           `json:"retry_count_max"`
	FirstAttemptAt *time.Time     `json:"first_attempt_at"`
	LastAttemptAt  *time.Time     `json:"last_attempt_at"`
	FailureReasons []FailureGroup `json:"failure_reasons"`
}

// History is the read-only retry-chain view of one notification instance.
type History struct {
	Records []Record       `json:"records"`
	Summary HistorySummary `json:"summary"`
}

// NewNotificationID validates the notification instance identifier from the
// path. A missing or all-whitespace value is rejected; surrounding whitespace
// is removed from an otherwise valid identifier.
func NewNotificationID(raw string) (string, error) {
	notificationID := strings.TrimSpace(raw)
	if notificationID == "" {
		return "", ErrInvalidNotificationID
	}
	return notificationID, nil
}

// BuildHistory aggregates records already ordered by (occurred_at, id)
// ascending into the published history shape. It only summarizes registered
// attempts and never fabricates missing ones.
func BuildHistory(records []Record) History {
	summary := HistorySummary{
		StatusCounts:   StatusCounts{},
		FailureReasons: []FailureGroup{},
	}
	if len(records) == 0 {
		return History{Records: []Record{}, Summary: summary}
	}

	failureCounts := map[string]int{}
	retryMin := records[0].RetryCount
	retryMax := records[0].RetryCount
	for _, record := range records {
		if record.RetryCount < retryMin {
			retryMin = record.RetryCount
		}
		if record.RetryCount > retryMax {
			retryMax = record.RetryCount
		}
		switch record.Status {
		case StatusPending:
			summary.StatusCounts.Pending++
		case StatusRetrying:
			summary.StatusCounts.Retrying++
		case StatusSucceeded:
			summary.StatusCounts.Succeeded++
		case StatusFailed:
			summary.StatusCounts.Failed++
		}
		if (record.Status == StatusFailed || record.Status == StatusRetrying) &&
			record.FailureReason != "" {
			failureCounts[record.FailureReason]++
		}
	}

	reasons := make([]FailureGroup, 0, len(failureCounts))
	for reason, count := range failureCounts {
		reasons = append(reasons, FailureGroup{FailureReason: reason, AttemptCount: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].AttemptCount != reasons[j].AttemptCount {
			return reasons[i].AttemptCount > reasons[j].AttemptCount
		}
		return reasons[i].FailureReason < reasons[j].FailureReason
	})
	summary.FailureReasons = reasons
	summary.TotalAttempts = len(records)
	summary.RetryCountMin = &retryMin
	summary.RetryCountMax = &retryMax
	first := records[0].OccurredAt
	last := records[len(records)-1].OccurredAt
	summary.FirstAttemptAt = &first
	summary.LastAttemptAt = &last
	return History{Records: records, Summary: summary}
}
