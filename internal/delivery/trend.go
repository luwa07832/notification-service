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

// TrendInput carries the raw delivery-trend parameters as received on the
// query string. The pointer field distinguishes an omitted template_id from
// an empty one: a present but blank value is invalid, while nil means the
// filter is unused.
type TrendInput struct {
	Channel    string
	Start      string
	End        string
	Interval   string
	TemplateID *string
}

// TrendQuery is the validated delivery-trend query. It shares the channel and
// half-open [Start, End) semantics with the basic list query; Start and End
// are guaranteed to fall on UTC bucket boundaries for the chosen Interval.
type TrendQuery struct {
	Channel    string
	Start      time.Time
	End        time.Time
	Interval   string
	TemplateID *string
}

// TrendBucket aggregates the registered attempts of one consecutive UTC time
// bucket. RetryCountMin and RetryCountMax are nil when the bucket holds no
// attempt.
type TrendBucket struct {
	BucketStart    time.Time      `json:"bucket_start"`
	BucketEnd      time.Time      `json:"bucket_end"`
	TotalAttempts  int            `json:"total_attempts"`
	StatusCounts   StatusCounts   `json:"status_counts"`
	FailureReasons []FailureGroup `json:"failure_reasons"`
	RetryCountMin  *int           `json:"retry_count_min"`
	RetryCountMax  *int           `json:"retry_count_max"`
}

// BucketWidth returns the fixed UTC width of one bucket. UTC days are always
// 24 hours, so both published intervals map to a constant duration.
func (q TrendQuery) BucketWidth() time.Duration {
	if q.Interval == IntervalDay {
		return 24 * time.Hour
	}
	return time.Hour
}

// NewTrend validates the delivery-trend parameters. The range is [Start, End)
// split into consecutive UTC buckets: both bounds must fall exactly on an
// hour boundary for interval=hour or on a UTC midnight for interval=day.
func NewTrend(in TrendInput) (TrendQuery, error) {
	if !IsAllowedChannel(strings.TrimSpace(in.Channel)) || in.Channel != strings.TrimSpace(in.Channel) {
		return TrendQuery{}, ErrInvalidTrend
	}
	if in.Interval != IntervalHour && in.Interval != IntervalDay {
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
	if !start.Before(end) {
		return TrendQuery{}, ErrInvalidTrend
	}
	startUTC := start.UTC()
	endUTC := end.UTC()
	if !onBucketBoundary(startUTC, in.Interval) || !onBucketBoundary(endUTC, in.Interval) {
		return TrendQuery{}, ErrInvalidTrend
	}
	query := TrendQuery{
		Channel:  in.Channel,
		Start:    startUTC,
		End:      endUTC,
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

// onBucketBoundary reports whether t falls exactly on a UTC bucket boundary
// for the interval: the top of an hour, or additionally UTC midnight for day
// buckets.
func onBucketBoundary(t time.Time, interval string) bool {
	if t.Minute() != 0 || t.Second() != 0 || t.Nanosecond() != 0 {
		return false
	}
	if interval == IntervalDay && t.Hour() != 0 {
		return false
	}
	return true
}
