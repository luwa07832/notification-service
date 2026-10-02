package delivery

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidAlignment marks a failed template-alignment parameter validation.
// The HTTP layer maps it to the published INVALID_TEMPLATE_ALIGNMENT_QUERY
// error code.
var ErrInvalidAlignment = errors.New("invalid template alignment query")

const (
	// DefaultAlignmentPageSize is used when page_size is omitted.
	DefaultAlignmentPageSize = 50
	// MaxAlignmentPageSize caps how many issues one page may return.
	MaxAlignmentPageSize = 200
)

// Issue codes the template-alignment read entry can attach to a record.
const (
	IssueTemplateMissing    = "template_missing"
	IssueChannelUnsupported = "channel_unsupported"
	IssueTemplateDisabled   = "template_disabled"
)

// AlignmentInput carries the raw template-alignment parameters as received on
// the query string. The pointer field distinguishes an omitted template_id
// from an empty one: a present but blank value is invalid, while nil means the
// filter is unused.
type AlignmentInput struct {
	Channel    string
	Start      string
	End        string
	TemplateID *string
	Page       *string
	PageSize   *string
}

// AlignmentQuery is the validated template-alignment query. It shares the
// channel and half-open [Start, End) semantics with the basic list query and
// adds an optional exact template identifier filter plus one-based pagination.
type AlignmentQuery struct {
	Channel    string
	Start      time.Time
	End        time.Time
	TemplateID *string
	Page       int
	PageSize   int
}

// NewAlignment validates every template-alignment parameter. The check itself
// only reads registered records and the current template registry; neither is
// modified by validating the query.
func NewAlignment(in AlignmentInput) (AlignmentQuery, error) {
	if !IsAllowedChannel(strings.TrimSpace(in.Channel)) || in.Channel != strings.TrimSpace(in.Channel) {
		return AlignmentQuery{}, ErrInvalidAlignment
	}
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return AlignmentQuery{}, ErrInvalidAlignment
	}
	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return AlignmentQuery{}, ErrInvalidAlignment
	}
	if !start.Before(end) {
		return AlignmentQuery{}, ErrInvalidAlignment
	}

	query := AlignmentQuery{
		Channel:  in.Channel,
		Start:    start.UTC(),
		End:      end.UTC(),
		Page:     1,
		PageSize: DefaultAlignmentPageSize,
	}

	if in.TemplateID != nil {
		templateID := strings.TrimSpace(*in.TemplateID)
		if templateID == "" {
			return AlignmentQuery{}, ErrInvalidAlignment
		}
		query.TemplateID = &templateID
	}
	if in.Page != nil {
		page, ok := parsePositiveInt(*in.Page)
		if !ok {
			return AlignmentQuery{}, ErrInvalidAlignment
		}
		query.Page = page
	}
	if in.PageSize != nil {
		pageSize, ok := parsePositiveInt(*in.PageSize)
		if !ok || pageSize > MaxAlignmentPageSize {
			return AlignmentQuery{}, ErrInvalidAlignment
		}
		query.PageSize = pageSize
	}
	return query, nil
}

// AlignmentIssue is one registered delivery record whose channel does not
// match the current template registry. IssueCode is exactly one of the
// published codes; records consistent with the registry are never returned.
type AlignmentIssue struct {
	ID            string    `json:"id"`
	TemplateID    string    `json:"template_id"`
	Channel       string    `json:"channel"`
	OccurredAt    time.Time `json:"occurred_at"`
	Status        string    `json:"status"`
	RetryCount    int       `json:"retry_count"`
	FailureReason string    `json:"failure_reason"`
	IssueCode     string    `json:"issue_code"`
}
