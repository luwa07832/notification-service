package delivery

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidSummary marks a failed failure-summary parameter validation. The
// HTTP layer maps it to the published INVALID_DELIVERY_SUMMARY error code.
var ErrInvalidSummary = errors.New("invalid delivery summary")

// SummaryInput carries the raw failure-summary parameters as received on the
// query string. TemplateID is a pointer so an omitted parameter is told apart
// from a present-but-blank one, which is invalid.
type SummaryInput struct {
	Channel    string
	Start      string
	End        string
	TemplateID *string
}

// SummaryQuery is the validated failure-summary query. It shares the channel
// and half-open [Start, End) semantics with the basic list query and adds an
// optional exact template filter.
type SummaryQuery struct {
	Channel    string
	Start      time.Time
	End        time.Time
	TemplateID *string
}

// NewSummary validates every summary parameter. The range is [start, end), so
// start must be strictly before end; a template_id passed explicitly must not
// be blank.
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
