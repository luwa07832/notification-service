package delivery

import (
	"testing"
	"time"
)

func validTrendInput() TrendInput {
	return TrendInput{
		Channel:  "sms",
		Start:    "2026-10-01T10:00:00Z",
		End:      "2026-10-01T12:00:00Z",
		Interval: IntervalHour,
	}
}

func TestNewTrendAcceptsHourAndDayIntervals(t *testing.T) {
	query, err := NewTrend(validTrendInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Channel != "sms" || query.Interval != IntervalHour || query.TemplateID != nil {
		t.Fatalf("query = %+v", query)
	}

	in := validTrendInput()
	in.Interval = IntervalDay
	in.Start = "2026-10-01T00:00:00Z"
	in.End = "2026-10-03T00:00:00Z"
	query, err = NewTrend(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Interval != IntervalDay {
		t.Fatalf("interval = %q", query.Interval)
	}
}

func TestNewTrendNormalizesOffsetToUTCBoundaries(t *testing.T) {
	in := validTrendInput()
	in.Start = "2026-10-01T18:00:00+08:00"
	in.End = "2026-10-01T20:00:00+08:00"
	query, err := NewTrend(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantStart := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if !query.Start.Equal(wantStart) || !query.End.Equal(wantEnd) {
		t.Fatalf("range = %v..%v, want %v..%v", query.Start, query.End, wantStart, wantEnd)
	}
}

func TestNewTrendTrimsOptionalTemplateID(t *testing.T) {
	in := validTrendInput()
	in.TemplateID = strPtr("  tpl-1 ")
	query, err := NewTrend(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template id = %v, want trimmed tpl-1", query.TemplateID)
	}
}

func TestNewTrendRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TrendInput)
	}{
		{"missing channel", func(in *TrendInput) { in.Channel = "" }},
		{"unknown channel", func(in *TrendInput) { in.Channel = "fax" }},
		{"padded channel", func(in *TrendInput) { in.Channel = " sms" }},
		{"missing start", func(in *TrendInput) { in.Start = "" }},
		{"missing end", func(in *TrendInput) { in.End = "" }},
		{"bad start format", func(in *TrendInput) { in.Start = "soon" }},
		{"bad end format", func(in *TrendInput) { in.End = "2026-10-01" }},
		{"start after end", func(in *TrendInput) {
			in.Start = "2026-10-01T12:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"start equals end", func(in *TrendInput) {
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"missing interval", func(in *TrendInput) { in.Interval = "" }},
		{"unknown interval", func(in *TrendInput) { in.Interval = "week" }},
		{"padded interval", func(in *TrendInput) { in.Interval = " hour" }},
		{"hour start off boundary", func(in *TrendInput) {
			in.Start = "2026-10-01T10:30:00Z"
		}},
		{"hour end off boundary", func(in *TrendInput) {
			in.End = "2026-10-01T12:30:00Z"
		}},
		{"day start off boundary", func(in *TrendInput) {
			in.Interval = IntervalDay
			in.Start = "2026-10-01T01:00:00Z"
			in.End = "2026-10-03T00:00:00Z"
		}},
		{"day end off boundary", func(in *TrendInput) {
			in.Interval = IntervalDay
			in.Start = "2026-10-01T00:00:00Z"
			in.End = "2026-10-03T12:00:00Z"
		}},
		{"offset start off utc boundary", func(in *TrendInput) {
			in.Start = "2026-10-01T18:30:00+08:00"
		}},
		{"empty template id", func(in *TrendInput) { in.TemplateID = strPtr("") }},
		{"blank template id", func(in *TrendInput) { in.TemplateID = strPtr("   ") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validTrendInput()
			tc.mutate(&in)
			if _, err := NewTrend(in); err != ErrInvalidTrend {
				t.Fatalf("err = %v, want ErrInvalidTrend", err)
			}
		})
	}
}
