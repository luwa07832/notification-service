package delivery

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidSummary marks a failed failure-summary parameter validation. The
// HTTP layer maps it to the published INVALID_DELIVERY_SUMMARY error code.
var ErrInvalidSummary = errors.New("invalid delivery failure summary")

// SummaryInput carries the raw failure-summary parameters as received on the
// query string. The pointer field distinguishes an omitted template_id from an
// empty one: a present but blank value is invalid, while nil means the filter
// is unused.
type SummaryInput struct {
	Channel    string
	Start      string
	End        string
	TemplateID *string
}

// SummaryQuery is the validated failure-summary query. It shares the channel
// and half-open [Start, End) semantics with the basic list query and adds an
// optional exact template identifier filter.
type SummaryQuery struct {
	Channel    string
	Start      time.Time
	End        time.Time
	TemplateID *string
}

// FailureGroup counts the attempts whose stored failure reason matches one
// exact, unnormalized piece of text.
type FailureGroup struct {
	FailureReason string `json:"failure_reason"`
	AttemptCount  int    `json:"attempt_count"`
}

// NewSummary validates the failure-summary parameters. Only failed or retrying
// records with a non-empty failure reason can ever be counted; the storage
// layer enforces that part of the contract.
func NewSummary(in SummaryInput) (SummaryQuery, error) {
	if !IsAllowedChannel(strings.TrimSpace(in.Channel)) || in.Channel != strings.TrimSpace(in.Channel) {
		return SummaryQuery{}, ErrInvalidSummary
	}
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return SummaryQuery{}, ErrInvalidSummary
	}
	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return SummaryQuery{}, ErrInvalidSummary
	}
	if !start.Before(end) {
		return SummaryQuery{}, ErrInvalidSummary
	}
	query := SummaryQuery{
		Channel: in.Channel,
		Start:   start.UTC(),
		End:     end.UTC(),
	}
	if in.TemplateID != nil {
		templateID := strings.TrimSpace(*in.TemplateID)
		if templateID == "" {
			return SummaryQuery{}, ErrInvalidSummary
		}
		query.TemplateID = &templateID
	}
	return query, nil
}
