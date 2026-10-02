// Package template owns the notification-template contract: the fields
// registered through the public write entry, the rules every template must
// satisfy, and the read-only list/detail filters.
package template

import (
	"errors"
	"strings"

	"github.com/luwa07832/notification-service/internal/delivery"
)

// MinChannels and MaxChannels bound the supported channel set of one template.
const (
	MinChannels = 1
	MaxChannels = 4
)

// Sentinel validation errors. The HTTP layer maps them to the published error
// codes.
var (
	ErrInvalidTemplate = errors.New("invalid notification template")
	ErrInvalidFilter   = errors.New("invalid notification template filter")
)

// Input is the payload submitted through the public write entry. Enabled is a
// pointer so a missing or null value is rejected the same way as a
// non-boolean value.
type Input struct {
	TemplateID string   `json:"template_id"`
	Name       string   `json:"name"`
	Body       string   `json:"body"`
	Channels   []string `json:"channels"`
	Enabled    *bool    `json:"enabled"`
}

// Template is a successfully registered notification template. Registration is
// terminal: templates can never be modified or deleted.
type Template struct {
	TemplateID string   `json:"template_id"`
	Name       string   `json:"name"`
	Body       string   `json:"body"`
	Channels   []string `json:"channels"`
	Enabled    bool     `json:"enabled"`
}

// New validates input and builds the template as it will be stored. The
// identifier and the name are trimmed and must stay non-empty; the body keeps
// its original text but must contain at least one non-whitespace character;
// channels must be one to four distinct known channels and keep request
// order; enabled must be an actual JSON boolean.
func New(in Input) (Template, error) {
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

// normalizeChannels accepts exactly one to four distinct known channels in
// their exact published spelling (surrounding whitespace is rejected); the
// returned slice keeps the request order.
func normalizeChannels(raw []string) ([]string, bool) {
	if len(raw) < MinChannels || len(raw) > MaxChannels {
		return nil, false
	}
	seen := make(map[string]struct{}, len(raw))
	channels := make([]string, 0, len(raw))
	for _, channel := range raw {
		if !delivery.IsAllowedChannel(channel) || channel != strings.TrimSpace(channel) {
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

// Filter is a validated list filter. A nil channel means the channel filter
// is unused; a nil enabled pointer means the enabled filter is unused.
type Filter struct {
	Channel *string
	Enabled *bool
}

// ParseFilter validates the list query parameters. The channel filter must be
// one known channel with no surrounding whitespace; the enabled filter must
// be the literal text true or false. Unknown parameters are ignored.
func ParseFilter(channelRaw string, channelPresent bool, enabledRaw string, enabledPresent bool) (Filter, error) {
	var filter Filter
	if channelPresent {
		channel := strings.TrimSpace(channelRaw)
		if !delivery.IsAllowedChannel(channel) || channelRaw != channel {
			return Filter{}, ErrInvalidFilter
		}
		filter.Channel = &channel
	}
	if enabledPresent {
		var enabled bool
		switch enabledRaw {
		case "true":
			enabled = true
		case "false":
			enabled = false
		default:
			return Filter{}, ErrInvalidFilter
		}
		filter.Enabled = &enabled
	}
	return filter, nil
}
