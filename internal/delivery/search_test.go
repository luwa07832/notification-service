package delivery

import "testing"

func strPtr(v string) *string { return &v }

func validSearchInput() SearchInput {
	return SearchInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestNewSearchAcceptsBaseAndOptionalFilters(t *testing.T) {
	in := validSearchInput()
	in.TemplateID = strPtr("  tpl-1 ")
	in.Status = strPtr(StatusFailed)
	in.RetryMin = strPtr("1")
	in.RetryMax = strPtr("3")
	in.FailureReasonContain = strPtr("timeout")
	in.Page = strPtr("2")
	in.PageSize = strPtr("25")

	query, err := NewSearch(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Channel != "sms" {
		t.Fatalf("channel = %q", query.Channel)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template id = %v, want trimmed tpl-1", query.TemplateID)
	}
	if query.Status == nil || *query.Status != StatusFailed {
		t.Fatalf("status = %v", query.Status)
	}
	if query.RetryMin == nil || *query.RetryMin != 1 || query.RetryMax == nil || *query.RetryMax != 3 {
		t.Fatalf("retry bounds = %v, %v", query.RetryMin, query.RetryMax)
	}
	if query.FailureReasonContain == nil || *query.FailureReasonContain != "timeout" {
		t.Fatalf("failure reason contains = %v", query.FailureReasonContain)
	}
	if query.Page != 2 || query.PageSize != 25 {
		t.Fatalf("pagination = %d, %d", query.Page, query.PageSize)
	}
}

func TestNewSearchDefaultsPagination(t *testing.T) {
	query, err := NewSearch(validSearchInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Page != 1 {
		t.Fatalf("page = %d, want 1", query.Page)
	}
	if query.PageSize != DefaultSearchPageSize {
		t.Fatalf("page size = %d, want %d", query.PageSize, DefaultSearchPageSize)
	}
}

func TestNewSearchRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SearchInput)
	}{
		{"missing channel", func(in *SearchInput) { in.Channel = "" }},
		{"unknown channel", func(in *SearchInput) { in.Channel = "fax" }},
		{"whitespace channel", func(in *SearchInput) { in.Channel = " sms" }},
		{"missing start", func(in *SearchInput) { in.Start = "" }},
		{"missing end", func(in *SearchInput) { in.End = "" }},
		{"bad start", func(in *SearchInput) { in.Start = "soon" }},
		{"bad end", func(in *SearchInput) { in.End = "late" }},
		{"start equals end", func(in *SearchInput) {
			in.Start = "2026-10-01T10:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"start after end", func(in *SearchInput) {
			in.Start = "2026-10-01T11:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"empty template id", func(in *SearchInput) { in.TemplateID = strPtr("   ") }},
		{"unknown status", func(in *SearchInput) { in.Status = strPtr("done") }},
		{"empty status", func(in *SearchInput) { in.Status = strPtr("") }},
		{"retry min negative", func(in *SearchInput) { in.RetryMin = strPtr("-1") }},
		{"retry min fractional", func(in *SearchInput) { in.RetryMin = strPtr("1.5") }},
		{"retry min empty", func(in *SearchInput) { in.RetryMin = strPtr("") }},
		{"retry min non numeric", func(in *SearchInput) { in.RetryMin = strPtr("two") }},
		{"retry max negative", func(in *SearchInput) { in.RetryMax = strPtr("-2") }},
		{"retry bounds reversed", func(in *SearchInput) {
			in.RetryMin = strPtr("5")
			in.RetryMax = strPtr("2")
		}},
		{"equal retry bounds allowed", func(in *SearchInput) {
			in.RetryMin = strPtr("2")
			in.RetryMax = strPtr("2")
		}},
		{"empty failure reason filter", func(in *SearchInput) { in.FailureReasonContain = strPtr("") }},
		{"page zero", func(in *SearchInput) { in.Page = strPtr("0") }},
		{"page negative", func(in *SearchInput) { in.Page = strPtr("-1") }},
		{"page non numeric", func(in *SearchInput) { in.Page = strPtr("first") }},
		{"page empty", func(in *SearchInput) { in.Page = strPtr("") }},
		{"page size zero", func(in *SearchInput) { in.PageSize = strPtr("0") }},
		{"page size too large", func(in *SearchInput) { in.PageSize = strPtr("201") }},
		{"page size negative", func(in *SearchInput) { in.PageSize = strPtr("-5") }},
		{"page size empty", func(in *SearchInput) { in.PageSize = strPtr("") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validSearchInput()
			tc.mutate(&in)
			_, err := NewSearch(in)
			if tc.name == "equal retry bounds allowed" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != ErrInvalidSearch {
				t.Fatalf("err = %v, want ErrInvalidSearch", err)
			}
		})
	}
}

func TestNewSearchNormalizesRangeToUTC(t *testing.T) {
	in := validSearchInput()
	in.Start = "2026-10-01T18:00:00+08:00"
	in.End = "2026-10-01T19:00:00+08:00"
	query, err := NewSearch(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := query.Start.Format("2006-01-02T15:04:05Z"), "2026-10-01T10:00:00Z"; got != want {
		t.Fatalf("start = %s, want %s", got, want)
	}
}
