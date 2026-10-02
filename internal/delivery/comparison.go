package delivery

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidComparison marks a failed channel-comparison parameter
// validation. The HTTP layer maps it to the published
// INVALID_DELIVERY_COMPARISON error code.
var ErrInvalidComparison = errors.New("invalid delivery channel comparison")

const (
	// MinComparisonChannels is the smallest channel set a comparison accepts.
	MinComparisonChannels = 2
	// MaxComparisonChannels is the largest channel set a comparison accepts.
	MaxComparisonChannels = 4
)

// ComparisonInput carries the raw channel-comparison parameters as received
// on the query string. The pointer field distinguishes an omitted
// template_id from a blank one: a present but blank value is invalid, while
// nil means the filter is unused.
type ComparisonInput struct {
	Channels   string
	Start      string
	End        string
	TemplateID *string
}

// ComparisonQuery is the validated channel-comparison query. Channels keep
// the request order, and the half-open [Start, End) range is shared with the
// other read-only entries.
type ComparisonQuery struct {
	Channels   []string
	Start      time.Time
	End        time.Time
	TemplateID *string
}

// ComparisonChannel aggregates the registered attempts of one channel within
// the queried range. RetryCountMin and RetryCountMax are nil when the channel
// has no attempt.
type ComparisonChannel struct {
	TotalAttempts int          `json:"total_attempts"`
	StatusCounts  StatusCounts `json:"status_counts"`
	RetryCountMin *int         `json:"retry_count_min"`
	RetryCountMax *int         `json:"retry_count_max"`
}

// ComparisonFailureReason counts one exact, unnormalized failure reason
// across the compared channels. ChannelCounts carries one entry per requested
// channel in request order; channels without the reason keep a zero count.
type ComparisonFailureReason struct {
	FailureReason string         `json:"failure_reason"`
	ChannelCounts map[string]int `json:"channel_counts"`
	AttemptCount  int            `json:"attempt_count"`
}

// ChannelComparison is the read-only comparison result. Channels follow the
// request order and every requested channel always has a stats entry.
type ChannelComparison struct {
	Channels       []string                  `json:"channels"`
	TotalAttempts  map[string]int            `json:"total_attempts"`
	StatusCounts   map[string]StatusCounts   `json:"status_counts"`
	RetryCountMin  map[string]*int           `json:"retry_count_min"`
	RetryCountMax  map[string]*int           `json:"retry_count_max"`
	FailureReasons []ComparisonFailureReason `json:"failure_reasons"`
}

// NewComparison validates the channel-comparison parameters. Channels is a
// comma-separated list of two to four distinct known channels with no
// surrounding whitespace; the range is [start, end) so start must be
// strictly before end.
func NewComparison(in ComparisonInput) (ComparisonQuery, error) {
	channels, ok := parseComparisonChannels(in.Channels)
	if !ok {
		return ComparisonQuery{}, ErrInvalidComparison
	}
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return ComparisonQuery{}, ErrInvalidComparison
	}
	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return ComparisonQuery{}, ErrInvalidComparison
	}
	if !start.Before(end) {
		return ComparisonQuery{}, ErrInvalidComparison
	}
	query := ComparisonQuery{
		Channels: channels,
		Start:    start.UTC(),
		End:      end.UTC(),
	}
	if in.TemplateID != nil {
		templateID := strings.TrimSpace(*in.TemplateID)
		if templateID == "" {
			return ComparisonQuery{}, ErrInvalidComparison
		}
		query.TemplateID = &templateID
	}
	return query, nil
}

// parseComparisonChannels accepts exactly two to four comma-separated known
// channels with no duplicate and no whitespace in any token or around it.
func parseComparisonChannels(raw string) ([]string, bool) {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return nil, false
	}
	parts := strings.Split(raw, ",")
	if len(parts) < MinComparisonChannels || len(parts) > MaxComparisonChannels {
		return nil, false
	}
	seen := make(map[string]struct{}, len(parts))
	channels := make([]string, 0, len(parts))
	for _, part := range parts {
		if !IsAllowedChannel(part) {
			return nil, false
		}
		if _, dup := seen[part]; dup {
			return nil, false
		}
		seen[part] = struct{}{}
		channels = append(channels, part)
	}
	return channels, true
}
