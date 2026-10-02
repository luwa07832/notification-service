package delivery

import (
	"strings"
	"testing"
)

func validComparisonInput() ComparisonInput {
	return ComparisonInput{
		Channels: "sms,email",
		Start:    "2026-10-01T10:00:00Z",
		End:      "2026-10-01T11:00:00Z",
	}
}

func TestNewComparisonAcceptsValidChannelLists(t *testing.T) {
	cases := []string{"sms,email", "push,in_app", "sms,email,push,in_app"}
	for _, raw := range cases {
		in := validComparisonInput()
		in.Channels = raw
		query, err := NewComparison(in)
		if err != nil {
			t.Fatalf("channels %q: unexpected error: %v", raw, err)
		}
		want := strings.Split(raw, ",")
		if len(query.Channels) != len(want) {
			t.Fatalf("channels = %v, want %v", query.Channels, want)
		}
		for i := range want {
			if query.Channels[i] != want[i] {
				t.Fatalf("channel order = %v, want %v", query.Channels, want)
			}
		}
	}

	in := validComparisonInput()
	in.TemplateID = strPtr("  tpl-1 ")
	query, err := NewComparison(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query.TemplateID == nil || *query.TemplateID != "tpl-1" {
		t.Fatalf("template id = %v, want trimmed tpl-1", query.TemplateID)
	}
}

func TestNewComparisonRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ComparisonInput)
	}{
		{"missing channels", func(in *ComparisonInput) { in.Channels = "" }},
		{"single channel", func(in *ComparisonInput) { in.Channels = "sms" }},
		{"too many channels", func(in *ComparisonInput) { in.Channels = "sms,email,push,in_app,sms" }},
		{"unknown channel", func(in *ComparisonInput) { in.Channels = "sms,fax" }},
		{"duplicate channels", func(in *ComparisonInput) { in.Channels = "sms,sms" }},
		{"leading whitespace element", func(in *ComparisonInput) { in.Channels = "sms, email" }},
		{"trailing whitespace element", func(in *ComparisonInput) { in.Channels = "sms ,email" }},
		{"whitespace around first", func(in *ComparisonInput) { in.Channels = " sms,email" }},
		{"empty channel element", func(in *ComparisonInput) { in.Channels = "sms," }},
		{"missing start", func(in *ComparisonInput) { in.Start = "" }},
		{"missing end", func(in *ComparisonInput) { in.End = "" }},
		{"bad start format", func(in *ComparisonInput) { in.Start = "2026-10-01 10:00:00" }},
		{"bad end format", func(in *ComparisonInput) { in.End = "not-a-time" }},
		{"start equals end", func(in *ComparisonInput) { in.End = in.Start }},
		{"start after end", func(in *ComparisonInput) {
			in.Start = "2026-10-01T11:00:00Z"
			in.End = "2026-10-01T10:00:00Z"
		}},
		{"empty template id", func(in *ComparisonInput) { in.TemplateID = strPtr("") }},
		{"blank template id", func(in *ComparisonInput) { in.TemplateID = strPtr("   ") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validComparisonInput()
			tc.mutate(&in)
			if _, err := NewComparison(in); err != ErrInvalidComparison {
				t.Fatalf("err = %v, want ErrInvalidComparison", err)
			}
		})
	}
}
