// Package segment validates an audience rule (JSON, usually written by the LLM)
// and turns it into a parameterized SQL WHERE clause. The rule never carries
// SQL: field and event names pick fixed fragments from the maps below and every
// value goes through a $n parameter.
package segment

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxDepth      = 3
	MaxConditions = 20
	MaxInValues   = 20
	MaxWithinDays = 365
	maxStringLen  = 64
)

// Node is a group (Match + Conditions), a field condition (Field + Op + Value)
// or an event condition (Event + WithinDays + Exists). Exactly one kind per node.
type Node struct {
	Match      string `json:"match,omitempty"`
	Conditions []Node `json:"conditions,omitempty"`

	Field string `json:"field,omitempty"`
	Op    string `json:"op,omitempty"`
	Value any    `json:"value,omitempty"`

	Event      string `json:"event,omitempty"`
	WithinDays int    `json:"within_days,omitempty"`
	Exists     *bool  `json:"exists,omitempty"`
}

// ValidationError lists every problem found, so one repair round can fix them all.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string { return strings.Join(e.Problems, "; ") }

type kind int

const (
	text kind = iota
	number
	date
)

var fields = map[string]struct {
	sql  string
	kind kind
}{
	"city":       {"u.city", text},
	"language":   {"u.language", text},
	"gender":     {"u.gender", text},
	"age":        {"(EXTRACT(YEAR FROM now())::int - u.birth_year)", number},
	"created_at": {"u.created_at", date},
}

// Text fields whose values are a closed set; anything else would match nobody
// without saying so (for example "erkek" instead of "male").
var allowedValues = map[string][]string{"gender": {"male", "female"}}

// Values returns the allowed values of a field, or nil when any text is fine.
func Values(field string) []string { return allowedValues[field] }

var ops = map[kind][]string{
	text:   {"eq", "neq", "in"},
	number: {"eq", "neq", "in", "gt", "gte", "lt", "lte"},
	date:   {"gt", "gte", "lt", "lte"},
}

var opSQL = map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}

var events = map[string]bool{"cart_add": true, "purchase": true, "page_view": true}

// Consent is enforced here, whatever the rule says.
var consentSQL = map[string]string{"sms": "u.consent_sms", "email": "u.consent_email"}

type builder struct {
	args     []any
	problems []string
	conds    int
}

// Build validates the rule and returns a WHERE clause over `users u` plus its
// arguments. The channel's consent flag is always part of the clause.
func Build(root Node, channel string) (string, []any, error) {
	consent, ok := consentSQL[channel]
	if !ok {
		return "", nil, &ValidationError{[]string{fmt.Sprintf("unknown channel %q", channel)}}
	}
	b := &builder{}
	var sql string
	if root.Match == "" && root.Conditions == nil {
		b.problem("rule", "root must be a group with match and conditions")
	} else {
		sql = b.group(root, "rule", 1)
	}
	if b.conds > MaxConditions {
		b.problem("rule", "has %d conditions, max %d", b.conds, MaxConditions)
	}
	if len(b.problems) > 0 {
		return "", nil, &ValidationError{b.problems}
	}
	return consent + " AND " + sql, b.args, nil
}

func (b *builder) problem(path, format string, a ...any) {
	b.problems = append(b.problems, path+": "+fmt.Sprintf(format, a...))
}

func (b *builder) param(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *builder) group(n Node, path string, depth int) string {
	if depth > MaxDepth {
		b.problem(path, "nested deeper than %d levels", MaxDepth)
		return ""
	}
	var sep string
	switch n.Match {
	case "all":
		sep = " AND "
	case "any":
		sep = " OR "
	default:
		b.problem(path, "match must be \"all\" or \"any\", got %q", n.Match)
	}
	if len(n.Conditions) == 0 {
		b.problem(path, "needs at least one condition")
	}
	var parts []string
	for i, c := range n.Conditions {
		if s := b.node(c, fmt.Sprintf("%s.conditions[%d]", path, i), depth); s != "" {
			parts = append(parts, s)
		}
	}
	return "(" + strings.Join(parts, sep) + ")"
}

func (b *builder) node(n Node, path string, depth int) string {
	isGroup := n.Match != "" || n.Conditions != nil
	switch {
	case isGroup && n.Field == "" && n.Event == "":
		return b.group(n, path, depth+1)
	case n.Field != "" && !isGroup && n.Event == "":
		b.conds++
		return b.field(n, path)
	case n.Event != "" && !isGroup && n.Field == "":
		b.conds++
		return b.event(n, path)
	}
	b.problem(path, "must be exactly one of: group, field condition, event condition")
	return ""
}

func (b *builder) field(n Node, path string) string {
	f, ok := fields[n.Field]
	if !ok {
		b.problem(path, "unknown field %q", n.Field)
		return ""
	}
	if !contains(ops[f.kind], n.Op) {
		b.problem(path, "op %q not allowed for field %q (allowed: %s)", n.Op, n.Field, strings.Join(ops[f.kind], ", "))
		return ""
	}
	v, err := coerce(f.kind, n.Op, n.Value)
	if err != nil {
		b.problem(path, "field %q: %v", n.Field, err)
		return ""
	}
	if allowed := allowedValues[n.Field]; allowed != nil {
		got, _ := v.(string)
		list, _ := v.([]string)
		for _, s := range append(list, got) {
			if s != "" && !contains(allowed, s) {
				b.problem(path, "field %q: value %q is not one of %s", n.Field, s, strings.Join(allowed, ", "))
				return ""
			}
		}
	}
	if n.Op == "in" {
		return f.sql + " = ANY(" + b.param(v) + ")"
	}
	return f.sql + " " + opSQL[n.Op] + " " + b.param(v)
}

func (b *builder) event(n Node, path string) string {
	if !events[n.Event] {
		b.problem(path, "unknown event %q", n.Event)
		return ""
	}
	if n.WithinDays < 1 || n.WithinDays > MaxWithinDays {
		b.problem(path, "within_days must be 1..%d, got %d", MaxWithinDays, n.WithinDays)
		return ""
	}
	if n.Exists == nil {
		b.problem(path, "event condition needs exists true or false")
		return ""
	}
	not := ""
	if !*n.Exists {
		not = "NOT "
	}
	return not + "EXISTS (SELECT 1 FROM events e WHERE e.user_id = u.id AND e.type = " + b.param(n.Event) +
		" AND e.at >= now() - make_interval(days => " + b.param(n.WithinDays) + "))"
}

// coerce checks the JSON value against the field type and returns the typed
// value to bind ([]string/[]int for "in").
func coerce(k kind, op string, v any) (any, error) {
	if op == "in" {
		list, ok := v.([]any)
		if !ok || len(list) == 0 || len(list) > MaxInValues {
			return nil, fmt.Errorf("value must be a list of 1..%d items", MaxInValues)
		}
		if k == number {
			out := make([]int, len(list))
			for i, e := range list {
				n, err := coerceInt(e)
				if err != nil {
					return nil, err
				}
				out[i] = n
			}
			return out, nil
		}
		out := make([]string, len(list))
		for i, e := range list {
			s, err := coerceText(e)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	}
	switch k {
	case number:
		return coerceInt(v)
	case date:
		s, _ := v.(string)
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, fmt.Errorf("value must be a date like 2025-01-31")
		}
		return t, nil
	}
	return coerceText(v)
}

func coerceText(v any) (string, error) {
	s, ok := v.(string)
	if !ok || s == "" || len([]rune(s)) > maxStringLen {
		return "", fmt.Errorf("value must be a string of 1..%d characters", maxStringLen)
	}
	return s, nil
}

func coerceInt(v any) (int, error) {
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) || f < 0 || f > 150 {
		return 0, fmt.Errorf("value must be a whole number 0..150")
	}
	return int(f), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Whitelist lists the allowed names, so the LLM's JSON schema and prompt are
// built from the same maps the validator uses.
func Whitelist() (fieldNames, opNames, eventNames []string) {
	for k := range fields {
		fieldNames = append(fieldNames, k)
	}
	for k := range opSQL {
		opNames = append(opNames, k)
	}
	opNames = append(opNames, "in")
	for k := range events {
		eventNames = append(eventNames, k)
	}
	sort.Strings(fieldNames)
	sort.Strings(opNames)
	sort.Strings(eventNames)
	return
}
