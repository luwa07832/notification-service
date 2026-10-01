package delivery

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidSearch marks a failed advanced-search parameter validation. The
// HTTP layer maps it to the published INVALID_DELIVERY_SEARCH error code.
var ErrInvalidSearch = errors.New("invalid delivery search")

const (
	// DefaultSearchPageSize is used when page_size is omitted.
	DefaultSearchPageSize = 50
	// MaxSearchPageSize caps how many records one page may return.
	MaxSearchPageSize = 200
)

// SearchInput carries the raw search parameters as received on the query
// string. Pointer fields distinguish an omitted parameter from an empty one:
// a present but empty value is invalid, while nil means the filter is unused.
type SearchInput struct {
	Channel              string
	Start                string
	End                  string
	TemplateID           *string
	Status               *string
	RetryMin             *string
	RetryMax             *string
	FailureReasonContain *string
	Page                 *string
	PageSize             *string
}

// SearchQuery is the validated advanced search. It shares the channel and
// half-open [Start, End) semantics with the basic list query and adds optional
// failure-tracking filters plus one-based pagination.
type SearchQuery struct {
	Channel              string
	Start                time.Time
	End                  time.Time
	TemplateID           *string
	Status               *string
	RetryMin             *int
	RetryMax             *int
	FailureReasonContain *string
	Page                 int
	PageSize             int
}

// NewSearch validates every search parameter. All supplied conditions must
// hold for a record to match, so even one malformed value rejects the request.
func NewSearch(in SearchInput) (SearchQuery, error) {
	if !IsAllowedChannel(strings.TrimSpace(in.Channel)) || in.Channel != strings.TrimSpace(in.Channel) {
		return SearchQuery{}, ErrInvalidSearch
	}
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return SearchQuery{}, ErrInvalidSearch
	}
	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return SearchQuery{}, ErrInvalidSearch
	}
	if !start.Before(end) {
		return SearchQuery{}, ErrInvalidSearch
	}

	query := SearchQuery{
		Channel:  in.Channel,
		Start:    start.UTC(),
		End:      end.UTC(),
		Page:     1,
		PageSize: DefaultSearchPageSize,
	}

	if in.TemplateID != nil {
		templateID := strings.TrimSpace(*in.TemplateID)
		if templateID == "" {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.TemplateID = &templateID
	}
	if in.Status != nil {
		if !IsAllowedStatus(*in.Status) {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.Status = in.Status
	}
	if in.RetryMin != nil {
		retryMin, ok := parseNonNegativeInt(*in.RetryMin)
		if !ok {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.RetryMin = &retryMin
	}
	if in.RetryMax != nil {
		retryMax, ok := parseNonNegativeInt(*in.RetryMax)
		if !ok {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.RetryMax = &retryMax
	}
	if query.RetryMin != nil && query.RetryMax != nil && *query.RetryMin > *query.RetryMax {
		return SearchQuery{}, ErrInvalidSearch
	}
	if in.FailureReasonContain != nil {
		if *in.FailureReasonContain == "" {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.FailureReasonContain = in.FailureReasonContain
	}
	if in.Page != nil {
		page, ok := parsePositiveInt(*in.Page)
		if !ok {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.Page = page
	}
	if in.PageSize != nil {
		pageSize, ok := parsePositiveInt(*in.PageSize)
		if !ok || pageSize > MaxSearchPageSize {
			return SearchQuery{}, ErrInvalidSearch
		}
		query.PageSize = pageSize
	}
	return query, nil
}

// parseNonNegativeInt accepts decimal non-negative integers only. Signs,
// whitespace, fractions and empty input are rejected.
func parseNonNegativeInt(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

// parsePositiveInt shares the strict integer parsing and additionally requires
// a value of at least one.
func parsePositiveInt(raw string) (int, bool) {
	value, ok := parseNonNegativeInt(raw)
	if !ok || value < 1 {
		return 0, false
	}
	return value, true
}
