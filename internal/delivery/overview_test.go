package delivery

import (
	"errors"
	"testing"
)

func validOverviewInput() OverviewInput {
	return OverviewInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestNewOverviewAcceptsBaseAndOptionalTemplateID(t *testing.T) {
	query, err := NewOverview(validOverviewInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Channel != "sms" || query.TemplateID != nil {
		t.Fatalf("query = %+v", query)
	}

	in := validOverviewInput()
	in.TemplateID = strPtr("  tpl-1 ")
	query, err = NewOverview(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template id = %v, want trimmed tpl-1", query.TemplateID)
	}
}

func TestNewOverviewRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*OverviewInput)
	}{
		{"missing channel", func(in *OverviewInput) { in.Channel = "" }},
		{"unknown channel", func(in *OverviewInput) { in.Channel = "fax" }},
		{"padded channel", func(in *OverviewInput) { in.Channel = " sms" }},
		{"missing start", func(in *OverviewInput) { in.Start = "" }},
		{"missing end", func(in *OverviewInput) { in.End = "" }},
		{"bad start format", func(in *OverviewInput) { in.Start = "soon" }},
		{"bad end format", func(in *OverviewInput) { in.End = "2026-10-01" }},
		{"start after end", func(in *OverviewInput) {
			in.Start = "2026-10-01T11:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"start equals end", func(in *OverviewInput) {
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"empty template id", func(in *OverviewInput) { in.TemplateID = strPtr("") }},
		{"blank template id", func(in *OverviewInput) { in.TemplateID = strPtr("   ") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validOverviewInput()
			tc.mutate(&in)
			if _, err := NewOverview(in); !errors.Is(err, ErrInvalidOverview) {
				t.Fatalf("err = %v, want ErrInvalidOverview", err)
			}
		})
	}
}
