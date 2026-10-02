package delivery

import (
	"errors"
	"testing"
	"time"
)

func validAlignmentInput() AlignmentInput {
	return AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T11:00:00Z",
	}
}

func TestNewAlignmentRequiresChannelStartEnd(t *testing.T) {
	base := validAlignmentInput()
	for i, mutate := range []func(*AlignmentInput){
		func(in *AlignmentInput) { in.Channel = "" },
		func(in *AlignmentInput) { in.Channel = "web" },
		func(in *AlignmentInput) { in.Channel = " sms" },
		func(in *AlignmentInput) { in.Start = "" },
		func(in *AlignmentInput) { in.Start = "not-a-time" },
		func(in *AlignmentInput) { in.End = "" },
		func(in *AlignmentInput) { in.End = "2026-10-01T11:00:00" },
	} {
		in := base
		mutate(&in)
		if _, err := NewAlignment(in); !errors.Is(err, ErrInvalidAlignment) {
			t.Fatalf("case %d: err = %v, want ErrInvalidAlignment", i, err)
		}
	}

	if _, err := NewAlignment(AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T11:00:00Z",
		End:     "2026-10-01T10:00:00Z",
	}); !errors.Is(err, ErrInvalidAlignment) {
		t.Fatalf("reversed range err = %v", err)
	}
	if _, err := NewAlignment(AlignmentInput{
		Channel: "sms",
		Start:   "2026-10-01T10:00:00Z",
		End:     "2026-10-01T10:00:00Z",
	}); !errors.Is(err, ErrInvalidAlignment) {
		t.Fatalf("empty range err = %v", err)
	}
}

func TestNewAlignmentDefaultsAndNormalization(t *testing.T) {
	query, err := NewAlignment(validAlignmentInput())
	if err != nil {
		t.Fatalf("new alignment: %v", err)
	}
	if query.Page != 1 || query.PageSize != DefaultAlignmentPageSize {
		t.Fatalf("defaults = page %d page_size %d", query.Page, query.PageSize)
	}
	if query.TemplateID != nil {
		t.Fatalf("template_id should be nil")
	}

	wantStart := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	if !query.Start.Equal(wantStart) {
		t.Fatalf("start = %v, want %v", query.Start, wantStart)
	}

	offsetInput := validAlignmentInput()
	offsetInput.Start = "2026-10-01T18:00:00+08:00"
	offsetInput.End = "2026-10-01T19:00:00+08:00"
	offsetQuery, err := NewAlignment(offsetInput)
	if err != nil {
		t.Fatalf("offset alignment: %v", err)
	}
	if !offsetQuery.Start.Equal(wantStart) || offsetQuery.Start.Location() != time.UTC {
		t.Fatalf("offset start = %v", offsetQuery.Start)
	}
}

func TestNewAlignmentTemplateID(t *testing.T) {
	for _, raw := range []string{"", " ", "\t\n"} {
		in := validAlignmentInput()
		in.TemplateID = &raw
		if _, err := NewAlignment(in); !errors.Is(err, ErrInvalidAlignment) {
			t.Fatalf("blank template_id %q err = %v", raw, err)
		}
	}

	in := validAlignmentInput()
	raw := "  tpl-1  "
	in.TemplateID = &raw
	query, err := NewAlignment(in)
	if err != nil {
		t.Fatalf("new alignment: %v", err)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template_id = %v", query.TemplateID)
	}
}

func TestNewAlignmentPagination(t *testing.T) {
	for _, mutate := range []func(*AlignmentInput){
		func(in *AlignmentInput) { page := "0"; in.Page = &page },
		func(in *AlignmentInput) { page := "-1"; in.Page = &page },
		func(in *AlignmentInput) { page := "1.5"; in.Page = &page },
		func(in *AlignmentInput) { pageSize := "0"; in.PageSize = &pageSize },
		func(in *AlignmentInput) { pageSize := "201"; in.PageSize = &pageSize },
		func(in *AlignmentInput) { pageSize := "abc"; in.PageSize = &pageSize },
	} {
		in := validAlignmentInput()
		mutate(&in)
		if _, err := NewAlignment(in); !errors.Is(err, ErrInvalidAlignment) {
			t.Fatalf("case err = %v", err)
		}
	}

	in := validAlignmentInput()
	page, pageSize := "3", "25"
	in.Page, in.PageSize = &page, &pageSize
	query, err := NewAlignment(in)
	if err != nil {
		t.Fatalf("new alignment: %v", err)
	}
	if query.Page != 3 || query.PageSize != 25 {
		t.Fatalf("pagination = page %d page_size %d", query.Page, query.PageSize)
	}
}
