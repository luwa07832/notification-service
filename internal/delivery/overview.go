package delivery

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidOverview marks a failed attempt-overview parameter validation. The
// HTTP layer maps it to the published INVALID_DELIVERY_OVERVIEW error code.
var ErrInvalidOverview = errors.New("invalid delivery attempt overview")

// OverviewInput carries the raw attempt-overview parameters as received on the
// query string. The pointer field distinguishes an omitted template_id from an
// empty one: a present but blank value is invalid, while nil means the filter
// is unused.
type OverviewInput struct {
	Channel    string
	Start      string
	End        string
	TemplateID *string
}

// OverviewQuery is the validated attempt-overview query. It shares the channel
// and half-open [Start, End) semantics with the basic list query and adds an
// optional exact template identifier filter.
type OverviewQuery struct {
	Channel    string
	Start      time.Time
	End        time.Time
	TemplateID *string
}

// StatusCounts always carries all four published statuses; a status with no
// matching attempt reports zero instead of disappearing.
type StatusCounts struct {
	Pending   int `json:"pending"`
	Retrying  int `json:"retrying"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// LastAttempt is the most recent registered attempt inside one template
// group: the record with the greatest occurred_at, breaking ties by the
// greatest id.
type LastAttempt struct {
	ID            string    `json:"id"`
	OccurredAt    time.Time `json:"occurred_at"`
	Status        string    `json:"status"`
	RetryCount    int       `json:"retry_count"`
	FailureReason string    `json:"failure_reason"`
}

// OverviewGroup aggregates every registered attempt of one template inside
// the queried channel and time range.
type OverviewGroup struct {
	TemplateID     string         `json:"template_id"`
	TotalAttempts  int            `json:"total_attempts"`
	RetryCountMin  int            `json:"retry_count_min"`
	RetryCountMax  int            `json:"retry_count_max"`
	StatusCounts   StatusCounts   `json:"status_counts"`
	LastAttempt    LastAttempt    `json:"last_attempt"`
	FailureReasons []FailureGroup `json:"failure_reasons"`
}

// NewOverview validates the attempt-overview parameters. The overview only
// reads registered records; the storage layer enforces that part of the
// contract.
func NewOverview(in OverviewInput) (OverviewQuery, error) {
	if !IsAllowedChannel(strings.TrimSpace(in.Channel)) || in.Channel != strings.TrimSpace(in.Channel) {
		return OverviewQuery{}, ErrInvalidOverview
	}
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return OverviewQuery{}, ErrInvalidOverview
	}
	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return OverviewQuery{}, ErrInvalidOverview
	}
	if !start.Before(end) {
		return OverviewQuery{}, ErrInvalidOverview
	}
	query := OverviewQuery{
		Channel: in.Channel,
		Start:   start.UTC(),
		End:     end.UTC(),
	}
	if in.TemplateID != nil {
		templateID := strings.TrimSpace(*in.TemplateID)
		if templateID == "" {
			return OverviewQuery{}, ErrInvalidOverview
		}
		query.TemplateID = &templateID
	}
	return query, nil
}
