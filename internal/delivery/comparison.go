package delivery

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidComparison marks a failed cross-channel comparison parameter
// validation. The HTTP layer maps it to the published
// INVALID_DELIVERY_COMPARISON error code.
var ErrInvalidComparison = errors.New("invalid delivery channel comparison")

// minComparisonChannels and maxComparisonChannels bound the number of channels
// a single cross-channel comparison may target.
const (
	minComparisonChannels = 2
	maxComparisonChannels = 4
)

// ComparisonInput carries the raw channel-comparison parameters as received on
// the query string. Channels stays the unsplit comma-separated value so its
// whitespace and duplication rules can be validated verbatim. The pointer
// field distinguishes an omitted template_id from a present blank one.
type ComparisonInput struct {
	Channels   string
	Start      string
	End        string
	TemplateID *string
}

// ComparisonQuery is the validated cross-channel comparison query. The
// half-open [Start, End) time range shares its meaning with the other read
// entries, and Channels keeps the requested order.
type ComparisonQuery struct {
	Channels   []string
	Start      time.Time
	End        time.Time
	TemplateID *string
}

// ComparisonFailureReason counts one exact, stored failure reason across every
// compared channel. ChannelCounts carries one entry per requested channel,
// including zero counts; AttemptCount is the sum across all of them.
type ComparisonFailureReason struct {
	FailureReason string         `json:"failure_reason"`
	ChannelCounts map[string]int `json:"channel_counts"`
	AttemptCount  int            `json:"attempt_count"`
}

// ChannelComparison aggregates registered attempts for the requested channels
// over the validated half-open range. The per-channel maps carry one entry
// per requested channel, and channels without attempts keep zero counts and
// null retry boundaries instead of being omitted.
type ChannelComparison struct {
	Channels       []string                  `json:"channels"`
	TotalAttempts  map[string]int            `json:"total_attempts"`
	StatusCounts   map[string]StatusCounts   `json:"status_counts"`
	RetryCountMin  map[string]*int           `json:"retry_count_min"`
	RetryCountMax  map[string]*int           `json:"retry_count_max"`
	FailureReasons []ComparisonFailureReason `json:"failure_reasons"`
}

// NewComparison validates the comparison parameters. Channels must be a
// comma-separated list of two to four known channels, with no duplicates and
// no surrounding or embedded whitespace. The comparison only reads registered
// records; it never writes, completes or modifies anything.
func NewComparison(in ComparisonInput) (ComparisonQuery, error) {
	parts := strings.Split(in.Channels, ",")
	if len(parts) < minComparisonChannels || len(parts) > maxComparisonChannels {
		return ComparisonQuery{}, ErrInvalidComparison
	}
	seen := make(map[string]struct{}, len(parts))
	channels := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part != strings.TrimSpace(part) {
			return ComparisonQuery{}, ErrInvalidComparison
		}
		if !IsAllowedChannel(part) {
			return ComparisonQuery{}, ErrInvalidComparison
		}
		if _, ok := seen[part]; ok {
			return ComparisonQuery{}, ErrInvalidComparison
		}
		seen[part] = struct{}{}
		channels = append(channels, part)
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
