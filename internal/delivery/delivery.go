// Package delivery defines the delivery-record contract: the fields every attempt records,
// the channels the service accepts and the validation rules for writes and queries.
package delivery

import (
	"errors"
	"strings"
	"time"
)

// Status values allowed for a delivery attempt.
const (
	StatusPending   = "pending"
	StatusRetrying  = "retrying"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// Channels the service can record deliveries for.
var channels = map[string]struct{}{
	"email": {},
	"sms":   {},
	"push":  {},
}

// ErrInvalid is returned by validation helpers for malformed writes or queries.
var ErrInvalid = errors.New("invalid delivery payload")

// Input is the payload accepted when registering a single delivery attempt.
type Input struct {
	TemplateID    string  `json:"template_id"`
	Channel       string  `json:"channel"`
	OccurredAt    string  `json:"occurred_at"`
	Status        string  `json:"status"`
	RetryCount    *int    `json:"retry_count"`
	FailureReason *string `json:"failure_reason"`
}

// Record is a registered delivery attempt and the body returned by every read entry.
type Record struct {
	ID            string  `json:"id"`
	TemplateID    string  `json:"template_id"`
	Channel       string  `json:"channel"`
	OccurredAt    string  `json:"occurred_at"`
	Status        string  `json:"status"`
	RetryCount    int     `json:"retry_count"`
	FailureReason *string `json:"failure_reason"`
}

// HasReason reports whether the given status carries a failure reason.
func HasReason(status string) bool {
	return status == StatusFailed || status == StatusRetrying
}

// ValidChannel reports whether channel names a supported target channel.
func ValidChannel(channel string) bool {
	_, ok := channels[channel]
	return ok
}

// ParseTime parses an RFC3339 timestamp. Validated inputs always parse; the store uses this
// helper so malformed values never reach the database.
func ParseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}

// ValidateWrite enforces the write contract and returns the normalized fields to persist.
func ValidateWrite(in Input) (time.Time, string, error) {
	if strings.TrimSpace(in.TemplateID) == "" {
		return time.Time{}, "", errors.New("template_id is required")
	}
	if !ValidChannel(in.Channel) {
		return time.Time{}, "", errors.New("channel must be one of email, sms, push")
	}
	occurredAt, err := ParseTime(in.OccurredAt)
	if err != nil {
		return time.Time{}, "", errors.New("occurred_at must be an RFC3339 timestamp")
	}
	switch in.Status {
	case StatusPending, StatusRetrying, StatusSucceeded, StatusFailed:
	default:
		return time.Time{}, "", errors.New("status must be one of pending, retrying, succeeded, failed")
	}
	if in.RetryCount == nil {
		return time.Time{}, "", errors.New("retry_count is required")
	}
	if *in.RetryCount < 0 {
		return time.Time{}, "", errors.New("retry_count must be a non-negative integer")
	}
	reason := in.FailureReason
	switch {
	case HasReason(in.Status):
		if reason == nil || strings.TrimSpace(*reason) == "" {
			return time.Time{}, "", errors.New("failure_reason is required when status is failed or retrying")
		}
	default:
		if reason != nil && strings.TrimSpace(*reason) != "" {
			return time.Time{}, "", errors.New("failure_reason must be empty when status is pending or succeeded")
		}
	}
	if !HasReason(in.Status) {
		reason = nil
	}
	return occurredAt, CanonicalTime(occurredAt), nil
}

// ValidateQuery enforces the read contract for channel-and-time-range queries.
func ValidateQuery(channel, start, end string) (time.Time, time.Time, error) {
	if !ValidChannel(channel) {
		return time.Time{}, time.Time{}, errors.New("channel must be one of email, sms, push")
	}
	startAt, err := ParseTime(start)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("start must be an RFC3339 timestamp")
	}
	endAt, err := ParseTime(end)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("end must be an RFC3339 timestamp")
	}
	if !startAt.Before(endAt) {
		return time.Time{}, time.Time{}, errors.New("start must be earlier than end")
	}
	return startAt, endAt, nil
}

// CanonicalTime renders the timestamp as fixed-fraction UTC. The format is zero padded, so
// its lexicographic order on disk matches chronological order and range comparisons stay
// textual. Equal instants normalize to the same string even with different input offsets.
func CanonicalTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}
