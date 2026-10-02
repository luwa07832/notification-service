package delivery

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// ErrInvalidNotificationID marks a blank or whitespace-only notification
// identifier in the delivery-history path. The HTTP layer maps it to the
// published INVALID_NOTIFICATION_ID error code.
var ErrInvalidNotificationID = errors.New("invalid notification id")

// NewNotificationHistoryQuery validates the notification identifier taken
// from the URL path. Only an empty or all-whitespace identifier is rejected;
// every other value is used exactly as supplied.
func NewNotificationHistoryQuery(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrInvalidNotificationID
	}
	return raw, nil
}

// HistorySummary aggregates one notification's registered attempts. The
// status counts always carry all four published statuses, and the retry and
// time bounds stay nil while the notification has no registered attempts.
type HistorySummary struct {
	TotalAttempts  int            `json:"total_attempts"`
	StatusCounts   StatusCounts   `json:"status_counts"`
	RetryCountMin  *int           `json:"retry_count_min"`
	RetryCountMax  *int           `json:"retry_count_max"`
	FirstAttemptAt *time.Time     `json:"first_attempt_at"`
	LastAttemptAt  *time.Time     `json:"last_attempt_at"`
	FailureReasons []FailureGroup `json:"failure_reasons"`
}

// NewHistorySummary builds the summary for one notification's records. The
// records must already be ordered by occurred_at ascending with ties broken
// by id ascending, which is the order the history query returns. Only failed
// or retrying records with a non-empty failure reason participate in the
// reason counts, and the stored reason text is grouped verbatim.
func NewHistorySummary(records []Record) HistorySummary {
	summary := HistorySummary{
		TotalAttempts:  len(records),
		FailureReasons: []FailureGroup{},
	}
	if len(records) == 0 {
		return summary
	}

	retryMin := records[0].RetryCount
	retryMax := records[0].RetryCount
	reasonCounts := map[string]int{}
	for _, record := range records {
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
		if record.RetryCount < retryMin {
			retryMin = record.RetryCount
		}
		if record.RetryCount > retryMax {
			retryMax = record.RetryCount
		}
		if (record.Status == StatusFailed || record.Status == StatusRetrying) && record.FailureReason != "" {
			reasonCounts[record.FailureReason]++
		}
	}
	summary.RetryCountMin = &retryMin
	summary.RetryCountMax = &retryMax
	firstAttemptAt := records[0].OccurredAt
	lastAttemptAt := records[len(records)-1].OccurredAt
	summary.FirstAttemptAt = &firstAttemptAt
	summary.LastAttemptAt = &lastAttemptAt

	reasons := make([]FailureGroup, 0, len(reasonCounts))
	for reason, count := range reasonCounts {
		reasons = append(reasons, FailureGroup{FailureReason: reason, AttemptCount: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].AttemptCount != reasons[j].AttemptCount {
			return reasons[i].AttemptCount > reasons[j].AttemptCount
		}
		return reasons[i].FailureReason < reasons[j].FailureReason
	})
	summary.FailureReasons = reasons
	return summary
}
