// Package template owns the notification-template contract: the fields a
// registered template carries and the rules every registration must satisfy.
package template

import (
	"errors"
	"strconv"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// Sentinel validation errors. The HTTP layer maps them to the published error
// codes.
var (
	ErrInvalidTemplate = errors.New("invalid notification template")
	ErrInvalidFilter   = errors.New("invalid notification template filter")
)

// TemplateInput is the payload submitted through the registration entry.
// Enabled is a pointer so a missing or null value is rejected the same way as
// a non-boolean value.
type TemplateInput struct {
	TemplateID string   `json:"template_id"`
	Name       string   `json:"name"`
	Body       string   `json:"body"`
	Channels   []string `json:"channels"`
	Enabled    *bool    `json:"enabled"`
}

// Template is a successfully registered notification template. Templates are
// immutable after registration; Channels keeps the submitted order.
type Template struct {
	TemplateID string   `json:"template_id"`
	Name       string   `json:"name"`
	Body       string   `json:"body"`
	Channels   []string `json:"channels"`
	Enabled    bool     `json:"enabled"`
}

// DeliverySummary aggregates the delivery facts of one template across every
// registered attempt. RetryCountMin and RetryCountMax are null when the
// template has no attempt.
type DeliverySummary struct {
	TotalAttempts  int                     `json:"total_attempts"`
	StatusCounts   delivery.StatusCounts   `json:"status_counts"`
	RetryCountMin  *int                    `json:"retry_count_min"`
	RetryCountMax  *int                    `json:"retry_count_max"`
	FailureReasons []delivery.FailureGroup `json:"failure_reasons"`
}

// Detail is a registered template together with its delivery summary.
type Detail struct {
	Template
	DeliverySummary DeliverySummary `json:"delivery_summary"`
}

// New validates the registration input and builds the template as it will be
// stored. TemplateID and name are trimmed before storage; the body is stored
// verbatim and only needs one non-whitespace character.
func New(in TemplateInput) (Template, error) {
	templateID := strings.TrimSpace(in.TemplateID)
	if templateID == "" {
		return Template{}, ErrInvalidTemplate
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Template{}, ErrInvalidTemplate
	}
	if strings.TrimSpace(in.Body) == "" {
		return Template{}, ErrInvalidTemplate
	}
	channels, ok := normalizeChannels(in.Channels)
	if !ok {
		return Template{}, ErrInvalidTemplate
	}
	if in.Enabled == nil {
		return Template{}, ErrInvalidTemplate
	}
	return Template{
		TemplateID: templateID,
		Name:       name,
		Body:       in.Body,
		Channels:   channels,
		Enabled:    *in.Enabled,
	}, nil
}

// normalizeChannels accepts one to four distinct allowed channels and returns
// them in their original order. A token with surrounding whitespace is
// rejected rather than trimmed.
func normalizeChannels(in []string) ([]string, bool) {
	if len(in) < 1 || len(in) > 4 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(in))
	channels := make([]string, 0, len(in))
	for _, channel := range in {
		if !delivery.IsAllowedChannel(channel) {
			return nil, false
		}
		if _, dup := seen[channel]; dup {
			return nil, false
		}
		seen[channel] = struct{}{}
		channels = append(channels, channel)
	}
	return channels, true
}

// ListFilter is a validated exact-match list filter. The pointer fields
// distinguish an omitted parameter from a present one: a present but invalid
// value must fail validation, while nil means the filter is unused.
type ListFilter struct {
	Channel *string
	Enabled *bool
}

// NewListFilter validates the list query parameters. Both filters are exact:
// channel must be a known channel with no whitespace and enabled must parse as
// a strict boolean literal.
func NewListFilter(channelRaw string, channelSet bool, enabledRaw string, enabledSet bool) (ListFilter, error) {
	var filter ListFilter
	if channelSet {
		channel := strings.TrimSpace(channelRaw)
		if !delivery.IsAllowedChannel(channel) || channelRaw != channel {
			return ListFilter{}, ErrInvalidFilter
		}
		filter.Channel = &channel
	}
	if enabledSet {
		if enabledRaw != "true" && enabledRaw != "false" {
			return ListFilter{}, ErrInvalidFilter
		}
		enabled, err := strconv.ParseBool(enabledRaw)
		if err != nil {
			return ListFilter{}, ErrInvalidFilter
		}
		filter.Enabled = &enabled
	}
	return filter, nil
}
