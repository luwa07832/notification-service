package delivery

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidSearch marks an invalid advanced-search parameter set. The HTTP
// layer maps it to the published INVALID_DELIVERY_SEARCH code.
var ErrInvalidSearch = errors.New("invalid delivery search")

// Published pagination defaults and bounds for the advanced search.
const (
	DefaultPage     = 1
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// SearchInput carries the raw query-string values for the advanced search.
// The Set flags distinguish an absent parameter from one supplied with an
// empty value; only the latter is a validation failure.
type SearchInput struct {
	Channel string
	Start   string
	End     string

	TemplateID    string
	TemplateIDSet bool

	Status    string
	StatusSet bool

	RetryMin    string
	RetryMinSet bool
	RetryMax    string
	RetryMaxSet bool

	FailureReasonContains    string
	FailureReasonContainsSet bool

	Page        string
	PageSet     bool
	PageSize    string
	PageSizeSet bool
}

// SearchQuery is a validated advanced search: the channel and half-open time
// range every request needs, plus the optional filters and the pagination
// window. A filter is active only when its Has flag is true.
type SearchQuery struct {
	Channel string
	Start   time.Time
	End     time.Time

	TemplateID    string
	HasTemplateID bool

	Status    string
	HasStatus bool

	RetryMin    int
	HasRetryMin bool
	RetryMax    int
	HasRetryMax bool

	FailureReasonContains    string
	HasFailureReasonContains bool

	Page     int
	PageSize int
}

// NewSearchQuery validates the raw parameters. Channel and the half-open
// [start, end) range follow the same rules as the simple channel query;
// every optional filter must be well formed when supplied.
func NewSearchQuery(in SearchInput) (SearchQuery, error) {
	base, err := NewQuery(in.Channel, in.Start, in.End)
	if err != nil {
		return SearchQuery{}, ErrInvalidSearch
	}
	query := SearchQuery{
		Channel:  base.Channel,
		Start:    base.Start,
		End:      base.End,
		Page:     DefaultPage,
		PageSize: DefaultPageSize,
	}

	if in.TemplateIDSet {
		templateID := strings.TrimSpace(in.TemplateID)
		if templateID == "" {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.TemplateID = templateID
		query.HasTemplateID = true
	}
	if in.StatusSet {
		if !IsAllowedStatus(in.Status) {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.Status = in.Status
		query.HasStatus = true
	}
	if in.RetryMinSet {
		value, err := parseSearchInt(in.RetryMin, 0, 0)
		if err != nil {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.RetryMin = value
		query.HasRetryMin = true
	}
	if in.RetryMaxSet {
		value, err := parseSearchInt(in.RetryMax, 0, 0)
		if err != nil {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.RetryMax = value
		query.HasRetryMax = true
	}
	if query.HasRetryMin && query.HasRetryMax && query.RetryMin > query.RetryMax {
		return SearchQuery{}, ErrInvalidSearch
	}
	if in.FailureReasonContainsSet {
		if in.FailureReasonContains == "" {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.FailureReasonContains = in.FailureReasonContains
		query.HasFailureReasonContains = true
	}
	if in.PageSet {
		page, err := parseSearchInt(in.Page, 1, 0)
		if err != nil {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.Page = page
	}
	if in.PageSizeSet {
		pageSize, err := parseSearchInt(in.PageSize, 1, MaxPageSize)
		if err != nil {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.PageSize = pageSize
	}
	return query, nil
}

// parseSearchInt parses a decimal integer and enforces min and, when max is
// positive, max. Anything that is not a plain integer is rejected.
func parseSearchInt(raw string, min, max int) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrInvalidSearch
	}
	if value < min || (max > 0 && value > max) {
		return 0, ErrInvalidSearch
	}
	return value, nil
}
