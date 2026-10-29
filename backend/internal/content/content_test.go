package content

import (
	"strings"
	"testing"
)

func TestSMSLimitCountsRunes(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{"160 ascii", strings.Repeat("a", 160), true},
		{"161 ascii", strings.Repeat("a", 161), false},
		{"160 turkish letters (320 bytes)", strings.Repeat("ş", 160), true},
		{"161 turkish letters", strings.Repeat("ş", 161), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(Message{Channel: "sms", Body: tt.body}, nil)
			if (len(got) == 0) != tt.ok {
				t.Errorf("ok=%v, problems=%v", tt.ok, got)
			}
		})
	}
}

func TestEmailSubjectLimit(t *testing.T) {
	ok := Message{Channel: "email", Subject: strings.Repeat("ğ", 80), Body: "hi"}
	if p := Check(ok, nil); len(p) != 0 {
		t.Errorf("80 should pass: %v", p)
	}
	bad := Message{Channel: "email", Subject: strings.Repeat("ğ", 81), Body: "hi"}
	if p := Check(bad, nil); len(p) != 1 || !strings.Contains(p[0], "subject is 81") {
		t.Errorf("81 should fail: %v", p)
	}
}

// 150 characters fit, but not once a long name is filled in.
func TestLongNameBreaksLimit(t *testing.T) {
	body := "{{first_name}} " + strings.Repeat("a", 135) // 14 + 1 + 135 = 150 raw
	m := Message{Channel: "sms", Body: body}
	short := map[string]string{"first_name": "Ali"}
	if p := Check(m, short); len(p) != 0 {
		t.Errorf("short name should pass: %v", p)
	}
	long := map[string]string{"first_name": strings.Repeat("Ş", 30)}
	if p := Check(m, long); len(p) != 1 || !strings.Contains(p[0], "after personalization") {
		t.Errorf("long name should fail: %v", p)
	}
}

func TestSubjectPersonalizationCounts(t *testing.T) {
	m := Message{Channel: "email", Subject: "{{first_name}} " + strings.Repeat("a", 70), Body: "x"}
	if p := Check(m, map[string]string{"first_name": strings.Repeat("b", 20)}); len(p) != 1 {
		t.Errorf("want subject over limit, got %v", p)
	}
}

func TestPlaceholders(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"unknown", "Hi {{email}}", "unknown placeholder {{email}}"},
		{"unclosed", "Hi {{first_name", "unbalanced"},
		{"stray close", "Hi first_name}}", "unbalanced"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Check(Message{Channel: "sms", Body: tt.body}, nil)
			if len(p) == 0 || !strings.Contains(strings.Join(p, ";"), tt.want) {
				t.Errorf("got %v, want %q", p, tt.want)
			}
		})
	}
	if p := Check(Message{Channel: "sms", Body: "Hi {{ first_name }} from {{city}}"}, nil); len(p) != 0 {
		t.Errorf("spaces inside braces are fine: %v", p)
	}
}

func TestEmptyAndUnknownChannel(t *testing.T) {
	if p := Check(Message{Channel: "email", Body: "x"}, nil); len(p) != 1 || !strings.Contains(p[0], "subject is empty") {
		t.Errorf("got %v", p)
	}
	if p := Check(Message{Channel: "sms"}, nil); len(p) != 1 || !strings.Contains(p[0], "body is empty") {
		t.Errorf("got %v", p)
	}
	if p := Check(Message{Channel: "push", Body: "x"}, nil); len(p) != 1 {
		t.Errorf("got %v", p)
	}
}

func TestRender(t *testing.T) {
	got := Render("Merhaba {{first_name}} {{ last_name }}, {{x}}", map[string]string{"first_name": "Ayşe", "last_name": "Kaya"})
	if got != "Merhaba Ayşe Kaya, {{x}}" {
		t.Errorf("got %q", got)
	}
}
