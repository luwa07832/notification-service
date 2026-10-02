package template

import "testing"

func boolPtr(v bool) *bool { return &v }

func TestNewTrimsAndStoresVerbatim(t *testing.T) {
	tpl, err := New(TemplateInput{
		TemplateID: "  tpl-1  ",
		Name:       "  Welcome ",
		Body:       "\n\t hello body \n",
		Channels:   []string{"in_app", "sms"},
		Enabled:    boolPtr(false),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if tpl.TemplateID != "tpl-1" || tpl.Name != "Welcome" {
		t.Fatalf("id/name not trimmed: %+v", tpl)
	}
	if tpl.Body != "\n\t hello body \n" {
		t.Fatalf("body must be stored verbatim: %q", tpl.Body)
	}
	if got := len(tpl.Channels); got != 2 || tpl.Channels[0] != "in_app" || tpl.Channels[1] != "sms" {
		t.Fatalf("channels must keep order: %v", tpl.Channels)
	}
	if tpl.Enabled {
		t.Fatalf("enabled = true, want false")
	}
}

func TestNewValidationFailures(t *testing.T) {
	valid := func() TemplateInput {
		return TemplateInput{
			TemplateID: "tpl-1",
			Name:       "Welcome",
			Body:       "hello",
			Channels:   []string{"sms"},
			Enabled:    boolPtr(true),
		}
	}
	cases := []struct {
		name string
		mut  func(*TemplateInput)
	}{
		{"empty template id", func(in *TemplateInput) { in.TemplateID = "   " }},
		{"missing template id", func(in *TemplateInput) { in.TemplateID = "" }},
		{"empty name", func(in *TemplateInput) { in.Name = " \t" }},
		{"blank body", func(in *TemplateInput) { in.Body = " \n\t " }},
		{"no channels", func(in *TemplateInput) { in.Channels = nil }},
		{"unknown channel", func(in *TemplateInput) { in.Channels = []string{"carrier-pigeon"} }},
		{"whitespace channel", func(in *TemplateInput) { in.Channels = []string{" sms"} }},
		{"duplicate channels", func(in *TemplateInput) { in.Channels = []string{"sms", "sms"} }},
		{"too many channels", func(in *TemplateInput) {
			in.Channels = []string{"sms", "email", "push", "in_app", "sms"}
		}},
		{"missing enabled", func(in *TemplateInput) { in.Enabled = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := valid()
			tc.mut(&in)
			if _, err := New(in); err != ErrInvalidTemplate {
				t.Fatalf("err = %v, want ErrInvalidTemplate", err)
			}
		})
	}
}

func TestNewAcceptsFourDistinctChannels(t *testing.T) {
	in := TemplateInput{
		TemplateID: "tpl-1",
		Name:       "Welcome",
		Body:       "hello",
		Channels:   []string{"sms", "email", "push", "in_app"},
		Enabled:    boolPtr(true),
	}
	tpl, err := New(in)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(tpl.Channels) != 4 {
		t.Fatalf("channels = %v", tpl.Channels)
	}
}

func TestNewListFilter(t *testing.T) {
	if _, err := NewListFilter("", false, "", false); err != nil {
		t.Fatalf("empty filter: %v", err)
	}

	filter, err := NewListFilter("email", true, "false", true)
	if err != nil {
		t.Fatalf("valid filter: %v", err)
	}
	if filter.Channel == nil || *filter.Channel != "email" {
		t.Fatalf("channel filter = %v", filter.Channel)
	}
	if filter.Enabled == nil || *filter.Enabled {
		t.Fatalf("enabled filter = %v, want false", filter.Enabled)
	}

	for _, channel := range []string{"bogus", " sms", ""} {
		if _, err := NewListFilter(channel, true, "", false); err != ErrInvalidFilter {
			t.Fatalf("channel %q: err = %v", channel, err)
		}
	}
	for _, enabled := range []string{"1", "TRUE", "yes", " false", ""} {
		if _, err := NewListFilter("", false, enabled, true); err != ErrInvalidFilter {
			t.Fatalf("enabled %q: err = %v", enabled, err)
		}
	}
}
