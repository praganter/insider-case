// Package content renders message templates and checks channel limits.
package content

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	SMSMax     = 160
	SubjectMax = 80
)

// Vars are the placeholders a template may use, e.g. {{first_name}}.
var Vars = []string{"first_name", "last_name", "city"}

type Message struct {
	Channel string `json:"channel"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

var placeholder = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// Render fills known placeholders; unknown ones are left as they are.
func Render(tmpl string, vars map[string]string) string {
	return placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		if v, ok := vars[strings.TrimSpace(m[2:len(m)-2])]; ok {
			return v
		}
		return m
	})
}

// Check returns the problems of one message once rendered with vars. Lengths
// count runes (not bytes), personalization included.
func Check(m Message, vars map[string]string) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, m.Channel+": "+fmt.Sprintf(format, a...)) }

	known := map[string]bool{}
	for _, v := range Vars {
		known[v] = true
	}
	for _, text := range []string{m.Subject, m.Body} {
		for _, p := range placeholder.FindAllStringSubmatch(text, -1) {
			if name := strings.TrimSpace(p[1]); !known[name] {
				add("unknown placeholder {{%s}}", name)
			}
		}
		if rest := placeholder.ReplaceAllString(text, ""); strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
			add("unbalanced {{ }} in %q", text)
		}
	}

	body := utf8.RuneCountInString(Render(m.Body, vars))
	switch m.Channel {
	case "sms":
		if body == 0 {
			add("body is empty")
		}
		if body > SMSMax {
			add("body is %d characters after personalization, max %d", body, SMSMax)
		}
	case "email":
		subject := utf8.RuneCountInString(Render(m.Subject, vars))
		if subject == 0 {
			add("subject is empty")
		}
		if subject > SubjectMax {
			add("subject is %d characters after personalization, max %d", subject, SubjectMax)
		}
		if body == 0 {
			add("body is empty")
		}
	default:
		add("unknown channel")
	}
	return problems
}
