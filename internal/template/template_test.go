package template

import "testing"

func boolPtr(v bool) *bool { return &v }

func validInput() Input {
	return Input{
		TemplateID: "tpl-1",
		Name:       "Login code",
		Body:       "Your code is 123456",
		Channels:   []string{"sms", "email"},
		Enabled:    boolPtr(true),
	}
}

func TestNewAcceptsValidTemplate(t *testing.T) {
	got, err := New(validInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TemplateID != "tpl-1" || got.Name != "Login code" || got.Body != "Your code is 123456" ||
		!got.Enabled || len(got.Channels) != 2 || got.Channels[0] != "sms" || got.Channels[1] != "email" {
		t.Fatalf("unexpected template: %#v", got)
	}
}

func TestNewTrimsIDAndNameButKeepsBodyVerbatim(t *testing.T) {
	in := validInput()
	in.TemplateID = "  tpl-2 \t"
	in.Name = "  Welcome "
	in.Body = "\t Hello \n"
	got, err := New(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TemplateID != "tpl-2" || got.Name != "Welcome" || got.Body != "\t Hello \n" {
		t.Fatalf("unexpected normalization: %#v", got)
	}
}

func TestNewRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Input)
	}{
		{"empty template id", func(in *Input) { in.TemplateID = "  " }},
		{"missing template id", func(in *Input) { in.TemplateID = "" }},
		{"empty name", func(in *Input) { in.Name = " \t" }},
		{"empty body", func(in *Input) { in.Body = "" }},
		{"blank body", func(in *Input) { in.Body = "  \n\t " }},
		{"missing channels", func(in *Input) { in.Channels = nil }},
		{"empty channels", func(in *Input) { in.Channels = []string{} }},
		{"too many channels", func(in *Input) { in.Channels = []string{"sms", "email", "push", "in_app", "sms"} }},
		{"unknown channel", func(in *Input) { in.Channels = []string{"carrier-pigeon"} }},
		{"whitespace channel token", func(in *Input) { in.Channels = []string{" sms", "email"} }},
		{"duplicate channels", func(in *Input) { in.Channels = []string{"sms", "sms"} }},
		{"duplicate channels after trim", func(in *Input) { in.Channels = []string{"sms", " sms "} }},
		{"missing enabled", func(in *Input) { in.Enabled = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mutate(&in)
			if _, err := New(in); err == nil {
				t.Fatalf("expected rejection for %s", tc.name)
			}
		})
	}
}

func TestNewChannelsKeepOrder(t *testing.T) {
	in := validInput()
	in.Channels = []string{"in_app", "sms"}
	got, err := New(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Channels[0] != "in_app" || got.Channels[1] != "sms" {
		t.Fatalf("channel order not preserved: %#v", got.Channels)
	}
}

func TestParseFilter(t *testing.T) {
	t.Run("no filters", func(t *testing.T) {
		filter, err := ParseFilter("", false, "", false)
		if err != nil || filter.Channel != nil || filter.Enabled != nil {
			t.Fatalf("unexpected: %#v err=%v", filter, err)
		}
	})
	t.Run("channel and enabled", func(t *testing.T) {
		filter, err := ParseFilter("sms", true, "false", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.Channel == nil || *filter.Channel != "sms" {
			t.Fatalf("channel filter wrong: %#v", filter.Channel)
		}
		if filter.Enabled == nil || *filter.Enabled {
			t.Fatalf("enabled filter wrong: %#v", filter.Enabled)
		}
	})
	t.Run("blank present channel is invalid", func(t *testing.T) {
		if _, err := ParseFilter("", true, "", false); err == nil {
			t.Fatal("expected error for present blank channel")
		}
	})
	t.Run("unknown channel is invalid", func(t *testing.T) {
		if _, err := ParseFilter("fax", true, "", false); err == nil {
			t.Fatal("expected error for unknown channel")
		}
	})
	t.Run("whitespace channel is invalid", func(t *testing.T) {
		if _, err := ParseFilter(" sms ", true, "", false); err == nil {
			t.Fatal("expected error for whitespace channel")
		}
	})
	t.Run("bad enabled is invalid", func(t *testing.T) {
		if _, err := ParseFilter("", false, "yes", true); err == nil {
			t.Fatal("expected error for non-boolean enabled")
		}
	})
	t.Run("blank enabled is invalid", func(t *testing.T) {
		if _, err := ParseFilter("", false, "", true); err == nil {
			t.Fatal("expected error for blank enabled")
		}
	})
}
