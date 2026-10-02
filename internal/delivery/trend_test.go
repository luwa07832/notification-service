package delivery

import (
	"errors"
	"testing"
	"time"
)

func validTrendInput() TrendInput {
	return TrendInput{
		Channel:  "sms",
		Start:    "2026-10-01T10:00:00Z",
		End:      "2026-10-01T13:00:00Z",
		Interval: IntervalHour,
	}
}

func TestNewTrendAcceptsHourInterval(t *testing.T) {
	query, err := NewTrend(validTrendInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Channel != "sms" || query.Interval != IntervalHour || query.TemplateID != nil {
		t.Fatalf("query = %+v", query)
	}
	if query.Start != time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("start = %v", query.Start)
	}
	if query.BucketWidth() != time.Hour {
		t.Fatalf("width = %v", query.BucketWidth())
	}
}

func TestNewTrendAcceptsDayIntervalAndTemplateID(t *testing.T) {
	in := validTrendInput()
	in.Interval = IntervalDay
	in.Start = "2026-10-01T00:00:00Z"
	in.End = "2026-10-03T00:00:00Z"
	in.TemplateID = strPtr("  tpl-1 ")
	query, err := NewTrend(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template id = %v, want trimmed tpl-1", query.TemplateID)
	}
	if query.BucketWidth() != 24*time.Hour {
		t.Fatalf("width = %v", query.BucketWidth())
	}
}

func TestNewTrendAcceptsOffsetTimesOnUTCBoundary(t *testing.T) {
	in := validTrendInput()
	in.Interval = IntervalDay
	in.Start = "2026-10-01T08:00:00+08:00"
	in.End = "2026-10-02T08:00:00+08:00"
	query, err := NewTrend(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.Start.Hour() != 0 || query.End.Hour() != 0 {
		t.Fatalf("bounds = %v..%v, want UTC midnights", query.Start, query.End)
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
		{"missing interval", func(in *TrendInput) { in.Interval = "" }},
		{"unknown interval", func(in *TrendInput) { in.Interval = "minute" }},
		{"padded interval", func(in *TrendInput) { in.Interval = "hour " }},
		{"uppercase interval", func(in *TrendInput) { in.Interval = "HOUR" }},
		{"missing start", func(in *TrendInput) { in.Start = "" }},
		{"missing end", func(in *TrendInput) { in.End = "" }},
		{"bad start format", func(in *TrendInput) { in.Start = "soon" }},
		{"bad end format", func(in *TrendInput) { in.End = "2026-10-01" }},
		{"start after end", func(in *TrendInput) {
			in.Start = "2026-10-01T13:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"start equals end", func(in *TrendInput) {
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"start off hour boundary", func(in *TrendInput) {
			in.Start = "2026-10-01T10:30:00Z"
		}},
		{"end off hour boundary", func(in *TrendInput) {
			in.End = "2026-10-01T13:00:01Z"
		}},
		{"start with sub-hour fraction", func(in *TrendInput) {
			in.Start = "2026-10-01T10:00:00.5Z"
		}},
		{"day start not midnight", func(in *TrendInput) {
			in.Interval = IntervalDay
			in.Start = "2026-10-01T10:00:00Z"
			in.End = "2026-10-02T00:00:00Z"
		}},
		{"day end not midnight", func(in *TrendInput) {
			in.Interval = IntervalDay
			in.Start = "2026-10-01T00:00:00Z"
			in.End = "2026-10-02T13:00:00Z"
		}},
		{"offset start off boundary", func(in *TrendInput) {
			in.Start = "2026-10-01T10:30:00+01:00"
		}},
		{"empty template id", func(in *TrendInput) { in.TemplateID = strPtr("") }},
		{"blank template id", func(in *TrendInput) { in.TemplateID = strPtr("   ") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validTrendInput()
			tc.mutate(&in)
			if _, err := NewTrend(in); !errors.Is(err, ErrInvalidTrend) {
				t.Fatalf("err = %v, want ErrInvalidTrend", err)
			}
		})
	}
}
