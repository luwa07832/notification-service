package delivery

import (
	"errors"
	"testing"
)

func validSummaryInput() SummaryInput {
	return SummaryInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestNewSummaryAcceptsBaseAndOptionalTemplateID(t *testing.T) {
	query, err := NewSummary(validSummaryInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Channel != "sms" || query.TemplateID != nil {
		t.Fatalf("query = %+v", query)
	}

	in := validSummaryInput()
	in.TemplateID = strPtr("  tpl-1 ")
	query, err = NewSummary(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template id = %v, want trimmed tpl-1", query.TemplateID)
	}
}

func TestNewSummaryRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SummaryInput)
	}{
		{"missing channel", func(in *SummaryInput) { in.Channel = "" }},
		{"unknown channel", func(in *SummaryInput) { in.Channel = "fax" }},
		{"padded channel", func(in *SummaryInput) { in.Channel = " sms" }},
		{"missing start", func(in *SummaryInput) { in.Start = "" }},
		{"missing end", func(in *SummaryInput) { in.End = "" }},
		{"bad start format", func(in *SummaryInput) { in.Start = "soon" }},
		{"bad end format", func(in *SummaryInput) { in.End = "2026-10-01" }},
		{"start after end", func(in *SummaryInput) {
			in.Start = "2026-10-01T11:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"start equals end", func(in *SummaryInput) {
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"empty template id", func(in *SummaryInput) { in.TemplateID = strPtr("") }},
		{"blank template id", func(in *SummaryInput) { in.TemplateID = strPtr("   ") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validSummaryInput()
			tc.mutate(&in)
			if _, err := NewSummary(in); !errors.Is(err, ErrInvalidSummary) {
				t.Fatalf("err = %v, want ErrInvalidSummary", err)
			}
		})
	}
}
