package delivery

import "testing"

func validSearchInput() SearchInput {
	return SearchInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestNewSearchQueryDefaultsPagination(t *testing.T) {
	query, err := NewSearchQuery(validSearchInput())
	if err != nil {
		t.Fatalf("valid search rejected: %v", err)
	}
	if query.Page != 1 || query.PageSize != 50 {
		t.Fatalf("page=%d page_size=%d, want 1 and 50", query.Page, query.PageSize)
	}
	if query.HasTemplateID || query.HasStatus || query.HasRetryMin || query.HasRetryMax ||
		query.HasFailureReasonContains {
		t.Fatalf("unexpected active filters: %#v", query)
	}
}

func TestNewSearchQueryAcceptsFullFilterSet(t *testing.T) {
	in := validSearchInput()
	in.TemplateID, in.TemplateIDSet = "  tpl-1\t", true
	in.Status, in.StatusSet = StatusFailed, true
	in.RetryMin, in.RetryMinSet = "1", true
	in.RetryMax, in.RetryMaxSet = "3", true
	in.FailureReasonContains, in.FailureReasonContainsSet = "Timeout", true
	in.Page, in.PageSet = "2", true
	in.PageSize, in.PageSizeSet = "200", true

	query, err := NewSearchQuery(in)
	if err != nil {
		t.Fatalf("valid search rejected: %v", err)
	}
	if query.TemplateID != "tpl-1" || !query.HasTemplateID {
		t.Fatalf("template id = %q, want trimmed tpl-1", query.TemplateID)
	}
	if query.Status != StatusFailed || query.RetryMin != 1 || query.RetryMax != 3 ||
		query.FailureReasonContains != "Timeout" || query.Page != 2 || query.PageSize != 200 {
		t.Fatalf("unexpected query: %#v", query)
	}
}

func TestNewSearchQueryRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SearchInput)
	}{
		{"missing channel", func(in *SearchInput) { in.Channel = "" }},
		{"unknown channel", func(in *SearchInput) { in.Channel = "fax" }},
		{"whitespace channel", func(in *SearchInput) { in.Channel = " sms" }},
		{"missing start", func(in *SearchInput) { in.Start = "" }},
		{"bad start", func(in *SearchInput) { in.Start = "soon" }},
		{"bad end", func(in *SearchInput) { in.End = "2026-10-01" }},
		{"start equals end", func(in *SearchInput) { in.End = in.Start }},
		{"start after end", func(in *SearchInput) { in.Start, in.End = in.End, in.Start }},
		{"empty template id", func(in *SearchInput) { in.TemplateIDSet = true }},
		{"blank template id", func(in *SearchInput) { in.TemplateID, in.TemplateIDSet = "   ", true }},
		{"empty status", func(in *SearchInput) { in.StatusSet = true }},
		{"unknown status", func(in *SearchInput) { in.Status, in.StatusSet = "done", true }},
		{"negative retry min", func(in *SearchInput) { in.RetryMin, in.RetryMinSet = "-1", true }},
		{"fractional retry min", func(in *SearchInput) { in.RetryMin, in.RetryMinSet = "1.5", true }},
		{"non numeric retry max", func(in *SearchInput) { in.RetryMax, in.RetryMaxSet = "many", true }},
		{"empty retry max", func(in *SearchInput) { in.RetryMaxSet = true }},
		{"inverted retry bounds", func(in *SearchInput) {
			in.RetryMin, in.RetryMinSet = "4", true
			in.RetryMax, in.RetryMaxSet = "2", true
		}},
		{"empty failure reason", func(in *SearchInput) { in.FailureReasonContainsSet = true }},
		{"zero page", func(in *SearchInput) { in.Page, in.PageSet = "0", true }},
		{"negative page", func(in *SearchInput) { in.Page, in.PageSet = "-2", true }},
		{"non numeric page", func(in *SearchInput) { in.Page, in.PageSet = "first", true }},
		{"empty page", func(in *SearchInput) { in.PageSet = true }},
		{"zero page size", func(in *SearchInput) { in.PageSize, in.PageSizeSet = "0", true }},
		{"oversized page size", func(in *SearchInput) { in.PageSize, in.PageSizeSet = "201", true }},
		{"non numeric page size", func(in *SearchInput) { in.PageSize, in.PageSizeSet = "all", true }},
		{"empty page size", func(in *SearchInput) { in.PageSizeSet = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validSearchInput()
			tc.mutate(&in)
			if _, err := NewSearchQuery(in); err != ErrInvalidSearch {
				t.Fatalf("err = %v, want ErrInvalidSearch", err)
			}
		})
	}
}

func TestNewSearchQueryAllowsOpenEndedRetryBounds(t *testing.T) {
	in := validSearchInput()
	in.RetryMin, in.RetryMinSet = "0", true
	if _, err := NewSearchQuery(in); err != nil {
		t.Fatalf("retry_min only rejected: %v", err)
	}
	in = validSearchInput()
	in.RetryMax, in.RetryMaxSet = "9", true
	if _, err := NewSearchQuery(in); err != nil {
		t.Fatalf("retry_max only rejected: %v", err)
	}
	in = validSearchInput()
	in.RetryMin, in.RetryMinSet = "2", true
	in.RetryMax, in.RetryMaxSet = "2", true
	if _, err := NewSearchQuery(in); err != nil {
		t.Fatalf("equal retry bounds rejected: %v", err)
	}
}
