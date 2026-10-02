package delivery

import "testing"

func validAlignmentInput() AlignmentInput {
	return AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestNewAlignmentAcceptsBaseAndOptionalTemplateID(t *testing.T) {
	in := validAlignmentInput()
	in.TemplateID = strPtr("  tpl-1 ")
	in.Page = strPtr("2")
	in.PageSize = strPtr("25")

	query, err := NewAlignment(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Channel != "sms" {
		t.Fatalf("channel = %q", query.Channel)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template id = %v, want trimmed tpl-1", query.TemplateID)
	}
	if query.Page != 2 || query.PageSize != 25 {
		t.Fatalf("pagination = %d, %d", query.Page, query.PageSize)
	}
}

func TestNewAlignmentDefaultsPagination(t *testing.T) {
	query, err := NewAlignment(validAlignmentInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Page != 1 {
		t.Fatalf("page = %d, want 1", query.Page)
	}
	if query.PageSize != DefaultAlignmentPageSize {
		t.Fatalf("page size = %d, want %d", query.PageSize, DefaultAlignmentPageSize)
	}
}

func TestNewAlignmentRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AlignmentInput)
	}{
		{"missing channel", func(in *AlignmentInput) { in.Channel = "" }},
		{"unknown channel", func(in *AlignmentInput) { in.Channel = "fax" }},
		{"whitespace channel", func(in *AlignmentInput) { in.Channel = " sms" }},
		{"missing start", func(in *AlignmentInput) { in.Start = "" }},
		{"missing end", func(in *AlignmentInput) { in.End = "" }},
		{"bad start", func(in *AlignmentInput) { in.Start = "soon" }},
		{"bad end", func(in *AlignmentInput) { in.End = "late" }},
		{"start equals end", func(in *AlignmentInput) {
			in.Start = "2026-10-01T10:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"start after end", func(in *AlignmentInput) {
			in.Start = "2026-10-01T11:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"blank template id", func(in *AlignmentInput) { in.TemplateID = strPtr("   ") }},
		{"page zero", func(in *AlignmentInput) { in.Page = strPtr("0") }},
		{"page negative", func(in *AlignmentInput) { in.Page = strPtr("-1") }},
		{"page fractional", func(in *AlignmentInput) { in.Page = strPtr("1.5") }},
		{"page size zero", func(in *AlignmentInput) { in.PageSize = strPtr("0") }},
		{"page size too large", func(in *AlignmentInput) { in.PageSize = strPtr("201") }},
		{"page size negative", func(in *AlignmentInput) { in.PageSize = strPtr("-3") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validAlignmentInput()
			tc.mutate(&in)
			if _, err := NewAlignment(in); err != ErrInvalidAlignment {
				t.Fatalf("err = %v, want ErrInvalidAlignment", err)
			}
		})
	}
}

func TestNewAlignmentAcceptsPageSizeBoundary(t *testing.T) {
	in := validAlignmentInput()
	in.PageSize = strPtr("200")
	query, err := NewAlignment(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.PageSize != MaxAlignmentPageSize {
		t.Fatalf("page size = %d, want %d", query.PageSize, MaxAlignmentPageSize)
	}
}
