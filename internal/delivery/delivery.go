// Package delivery owns the delivery-result record contract: the fields registered
// after every real delivery attempt and the rules every record must satisfy.
package delivery

import (
	"errors"
	"strings"
	"time"
)

// Sentinel validation errors. The HTTP layer maps them to the published error codes.
var (
	ErrInvalidRecord = errors.New("invalid delivery record")
	ErrInvalidBatch  = errors.New("invalid delivery record batch")
	ErrInvalidQuery  = errors.New("invalid delivery query")
)

// Batch size bounds for the bulk registration entry.
const (
	MinBatchSize = 2
	MaxBatchSize = 100
)

// Allowed delivery channels.
var channels = map[string]struct{}{
	"sms":    {},
	"email":  {},
	"push":   {},
	"in_app": {},
}

// IsAllowedChannel reports whether channel is one the service can register.
func IsAllowedChannel(channel string) bool {
	_, ok := channels[channel]
	return ok
}

// IsAllowedStatus reports whether status is one of the four published states.
func IsAllowedStatus(status string) bool {
	switch status {
	case StatusPending, StatusRetrying, StatusSucceeded, StatusFailed:
		return true
	}
	return false
}

// Published delivery statuses.
const (
	StatusPending   = "pending"
	StatusRetrying  = "retrying"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// RecordInput is the payload submitted through the public write entry.
// RetryCount is a pointer so a missing or null value is rejected the same
// way as a non-integer value.
type RecordInput struct {
	TemplateID    string `json:"template_id"`
	Channel       string `json:"channel"`
	OccurredAt    string `json:"occurred_at"`
	Status        string `json:"status"`
	RetryCount    *int   `json:"retry_count"`
	FailureReason string `json:"failure_reason"`
}

// BatchInput is the payload submitted through the bulk write entry. Every
// element is validated by the same rules as a single-record submission.
type BatchInput struct {
	Records []RecordInput `json:"records"`
}

// Record is a successfully registered delivery attempt. Every attempt is an
// independent row; repeated notifications never overwrite earlier records.
type Record struct {
	ID            string    `json:"id"`
	TemplateID    string    `json:"template_id"`
	Channel       string    `json:"channel"`
	OccurredAt    time.Time `json:"occurred_at"`
	Status        string    `json:"status"`
	RetryCount    int       `json:"retry_count"`
	FailureReason string    `json:"failure_reason"`
}

// NewRecord validates input and builds the record as it will be stored. The id
// is assigned by the storage layer when the insert actually succeeds.
func NewRecord(in RecordInput) (Record, error) {
	templateID := strings.TrimSpace(in.TemplateID)
	if templateID == "" {
		return Record{}, ErrInvalidRecord
	}
	if !IsAllowedChannel(strings.TrimSpace(in.Channel)) || in.Channel != strings.TrimSpace(in.Channel) {
		return Record{}, ErrInvalidRecord
	}
	occurredAt, err := time.Parse(time.RFC3339, in.OccurredAt)
	if err != nil {
		return Record{}, ErrInvalidRecord
	}
	if !IsAllowedStatus(in.Status) {
		return Record{}, ErrInvalidRecord
	}
	if in.RetryCount == nil || *in.RetryCount < 0 {
		return Record{}, ErrInvalidRecord
	}
	switch in.Status {
	case StatusFailed, StatusRetrying:
		if strings.TrimSpace(in.FailureReason) == "" {
			return Record{}, ErrInvalidRecord
		}
	case StatusPending, StatusSucceeded:
		if in.FailureReason != "" {
			return Record{}, ErrInvalidRecord
		}
	}
	return Record{
		TemplateID:    templateID,
		Channel:       in.Channel,
		OccurredAt:    occurredAt.UTC(),
		Status:        in.Status,
		RetryCount:    *in.RetryCount,
		FailureReason: in.FailureReason,
	}, nil
}

// NewBatch validates a bulk submission and builds the records in request
// order. The batch must contain between MinBatchSize and MaxBatchSize records,
// and a single invalid element rejects the whole batch before anything is
// registered.
func NewBatch(in BatchInput) ([]Record, error) {
	if len(in.Records) < MinBatchSize || len(in.Records) > MaxBatchSize {
		return nil, ErrInvalidBatch
	}
	records := make([]Record, len(in.Records))
	for i, item := range in.Records {
		record, err := NewRecord(item)
		if err != nil {
			return nil, ErrInvalidBatch
		}
		records[i] = record
	}
	return records, nil
}

// Query is a validated channel plus half-open time range query.
type Query struct {
	Channel string
	Start   time.Time
	End     time.Time
}

// NewQuery validates the query parameters. The range is [start, end), so start
// must be strictly before end.
func NewQuery(channel, startRaw, endRaw string) (Query, error) {
	if !IsAllowedChannel(strings.TrimSpace(channel)) || channel != strings.TrimSpace(channel) {
		return Query{}, ErrInvalidQuery
	}
	start, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		return Query{}, ErrInvalidQuery
	}
	end, err := time.Parse(time.RFC3339, endRaw)
	if err != nil {
		return Query{}, ErrInvalidQuery
	}
	if !start.Before(end) {
		return Query{}, ErrInvalidQuery
	}
	return Query{
		Channel: channel,
		Start:   start.UTC(),
		End:     end.UTC(),
	}, nil
}
