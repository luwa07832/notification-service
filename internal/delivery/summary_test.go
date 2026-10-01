package delivery

import "testing"

func validSummaryInput() SummaryInput {
	return SummaryInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestNewSummaryAcceptsBaseAndTemplateFilter(t *testing.T) {
	query, err := NewSummary(validSummaryInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Channel != "sms" || query.TemplateID != nil {
		t.Fatalf("query = %+v", query)
	}
	if query.Start.Format("2006-01-02T15:04:05Z") != "2026-10-01T10:00:00Z" ||
		query.End.Format("2006-01-02T15:04:05Z") != "2026-10-01T11:00:00Z" {
		t.Fatalf("range = %v..%v", query.Start, query.End)
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
	cases := map[string]func(*SummaryInput){
		"channel missing":        func(in *SummaryInput) { in.Channel = "" },
		"channel unknown":        func(in *SummaryInput) { in.Channel = "fax" },
		"channel padded":         func(in *SummaryInput) { in.Channel = " sms" },
		"start missing":          func(in *SummaryInput) { in.Start = "" },
		"start not rfc3339":      func(in *SummaryInput) { in.Start = "soon" },
		"end missing":            func(in *SummaryInput) { in.End = "" },
		"end not rfc3339":        func(in *SummaryInput) { in.End = "later" },
		"start equals end":       func(in *SummaryInput) { in.End = in.Start },
		"start after end":        func(in *SummaryInput) { in.Start, in.End = in.End, in.Start },
		"template id empty":      func(in *SummaryInput) { in.TemplateID = strPtr("") },
		"template id whitespace": func(in *SummaryInput) { in.TemplateID = strPtr("   ") },
	}
	for name, mutate := range cases {
		in := validSummaryInput()
		mutate(&in)
		if _, err := NewSummary(in); err == nil {
			t.Fatalf("%s: expected error, got none", name)
		}
	}
}

func TestNewSummaryAcceptsEveryChannel(t *testing.T) {
	for _, channel := range []string{"sms", "email", "push", "in_app"} {
		in := validSummaryInput()
		in.Channel = channel
		if _, err := NewSummary(in); err != nil {
			t.Fatalf("channel %s: unexpected error: %v", channel, err)
		}
	}
}
