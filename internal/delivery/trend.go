package delivery

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidTrend marks a failed delivery-trend parameter validation. The
// HTTP layer maps it to the published INVALID_DELIVERY_TREND error code.
var ErrInvalidTrend = errors.New("invalid delivery trend")

// Published trend bucket intervals.
const (
	IntervalHour = "hour"
	IntervalDay  = "day"
)

// TrendInput carries the raw trend parameters as received on the query
// string. The pointer field distinguishes an omitted template_id from an
// empty one: a present but blank value is invalid, while nil means the
// filter is unused.
type TrendInput struct {
	Channel    string
	Start      string
	End        string
	Interval   string
	TemplateID *string
}

// TrendQuery is the validated trend query. It shares the channel and
// half-open [Start, End) semantics with the basic list query, requires both
// endpoints to land on UTC bucket boundaries, and adds an optional exact
// template identifier filter.
type TrendQuery struct {
	Channel    string
	Start      time.Time
	End        time.Time
	Interval   string
	TemplateID *string
}

// TrendBucket aggregates every registered attempt inside one aligned UTC
// bucket. A bucket without attempts keeps zero counts, an empty failure
// reason list and null retry count extrema.
type TrendBucket struct {
	BucketStart    time.Time      `json:"bucket_start"`
	BucketEnd      time.Time      `json:"bucket_end"`
	TotalAttempts  int            `json:"total_attempts"`
	StatusCounts   StatusCounts   `json:"status_counts"`
	FailureReasons []FailureGroup `json:"failure_reasons"`
	RetryCountMin  *int           `json:"retry_count_min"`
	RetryCountMax  *int           `json:"retry_count_max"`
}

// NewTrend validates the trend parameters. Buckets follow UTC clock hours or
// UTC calendar days and start and end must both sit on bucket boundaries.
// The trend only reads registered records; it never writes, completes or
// fabricates anything.
func NewTrend(in TrendInput) (TrendQuery, error) {
	if !IsAllowedChannel(strings.TrimSpace(in.Channel)) || in.Channel != strings.TrimSpace(in.Channel) {
		return TrendQuery{}, ErrInvalidTrend
	}
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return TrendQuery{}, ErrInvalidTrend
	}
	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return TrendQuery{}, ErrInvalidTrend
	}
	start = start.UTC()
	end = end.UTC()
	if !start.Before(end) {
		return TrendQuery{}, ErrInvalidTrend
	}
	var bucketSize time.Duration
	switch in.Interval {
	case IntervalHour:
		bucketSize = time.Hour
	case IntervalDay:
		bucketSize = 24 * time.Hour
	default:
		return TrendQuery{}, ErrInvalidTrend
	}
	if !start.Equal(start.Truncate(bucketSize)) || !end.Equal(end.Truncate(bucketSize)) {
		return TrendQuery{}, ErrInvalidTrend
	}
	query := TrendQuery{
		Channel:  in.Channel,
		Start:    start,
		End:      end,
		Interval: in.Interval,
	}
	if in.TemplateID != nil {
		templateID := strings.TrimSpace(*in.TemplateID)
		if templateID == "" {
			return TrendQuery{}, ErrInvalidTrend
		}
		query.TemplateID = &templateID
	}
	return query, nil
}
