package segment

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func parse(t *testing.T, s string) Node {
	t.Helper()
	var n Node
	if err := json.Unmarshal([]byte(s), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBuildValid(t *testing.T) {
	tests := []struct {
		name, rule, channel, sql string
		args                     []any
	}{
		{
			"city eq",
			`{"match":"all","conditions":[{"field":"city","op":"eq","value":"İstanbul"}]}`,
			"sms",
			"u.consent_sms AND (u.city = $1)",
			[]any{"İstanbul"},
		},
		{
			"email channel uses email consent",
			`{"match":"all","conditions":[{"field":"language","op":"eq","value":"tr"}]}`,
			"email",
			"u.consent_email AND (u.language = $1)",
			[]any{"tr"},
		},
		{
			"in list and any",
			`{"match":"any","conditions":[{"field":"city","op":"in","value":["Ankara","İzmir"]},{"field":"age","op":"gte","value":30}]}`,
			"sms",
			"u.consent_sms AND (u.city = ANY($1) OR (EXTRACT(YEAR FROM now())::int - u.birth_year) >= $2)",
			[]any{[]string{"Ankara", "İzmir"}, 30},
		},
		{
			"events and nesting",
			`{"match":"all","conditions":[
				{"event":"cart_add","within_days":30,"exists":true},
				{"event":"purchase","within_days":30,"exists":false},
				{"match":"any","conditions":[{"field":"gender","op":"neq","value":"male"}]}]}`,
			"sms",
			"u.consent_sms AND (" +
				"EXISTS (SELECT 1 FROM events e WHERE e.user_id = u.id AND e.type = $1 AND e.at >= now() - make_interval(days => $2)) AND " +
				"NOT EXISTS (SELECT 1 FROM events e WHERE e.user_id = u.id AND e.type = $3 AND e.at >= now() - make_interval(days => $4)) AND " +
				"(u.gender <> $5))",
			[]any{"cart_add", 30, "purchase", 30, "male"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := Build(parse(t, tt.rule), tt.channel)
			if err != nil {
				t.Fatal(err)
			}
			if sql != tt.sql {
				t.Errorf("sql\n got %s\nwant %s", sql, tt.sql)
			}
			if !reflect.DeepEqual(args, tt.args) {
				t.Errorf("args got %#v want %#v", args, tt.args)
			}
		})
	}
}

func TestBuildInvalid(t *testing.T) {
	tests := []struct{ name, rule, channel, want string }{
		{"root is a condition", `{"field":"city","op":"eq","value":"x"}`, "sms", "root must be a group"},
		{"bad match", `{"match":"some","conditions":[{"field":"city","op":"eq","value":"x"}]}`, "sms", "match must be"},
		{"empty group", `{"match":"all","conditions":[]}`, "sms", "at least one condition"},
		{"unknown field", `{"match":"all","conditions":[{"field":"password","op":"eq","value":"x"}]}`, "sms", `unknown field "password"`},
		{"op not allowed for field", `{"match":"all","conditions":[{"field":"city","op":"gt","value":"x"}]}`, "sms", `op "gt" not allowed`},
		{"wrong value type", `{"match":"all","conditions":[{"field":"age","op":"gt","value":"old"}]}`, "sms", "whole number"},
		{"fractional age", `{"match":"all","conditions":[{"field":"age","op":"gt","value":30.5}]}`, "sms", "whole number"},
		{"language outside the set", `{"match":"all","conditions":[{"field":"language","op":"eq","value":"de"}]}`, "sms", `value "de" is not one of tr, en`},
		{"gender outside the set", `{"match":"all","conditions":[{"field":"gender","op":"eq","value":"erkek"}]}`, "sms", `value "erkek" is not one of male, female`},
		{"gender list outside the set", `{"match":"all","conditions":[{"field":"gender","op":"in","value":["male","other"]}]}`, "sms", `value "other" is not one of`},
		{"in needs list", `{"match":"all","conditions":[{"field":"city","op":"in","value":"Ankara"}]}`, "sms", "list of 1.."},
		{"bad date", `{"match":"all","conditions":[{"field":"created_at","op":"gt","value":"yesterday"}]}`, "sms", "date like"},
		{"unknown event", `{"match":"all","conditions":[{"event":"login","within_days":7,"exists":true}]}`, "sms", `unknown event "login"`},
		{"within_days too big", `{"match":"all","conditions":[{"event":"purchase","within_days":999,"exists":true}]}`, "sms", "within_days must be 1..365"},
		{"within_days missing", `{"match":"all","conditions":[{"event":"purchase","exists":true}]}`, "sms", "within_days must be"},
		{"exists missing", `{"match":"all","conditions":[{"event":"purchase","within_days":7}]}`, "sms", "needs exists"},
		{"mixed node", `{"match":"all","conditions":[{"field":"city","op":"eq","value":"x","event":"purchase"}]}`, "sms", "exactly one of"},
		{"empty node", `{"match":"all","conditions":[{}]}`, "sms", "exactly one of"},
		{"unknown channel", `{"match":"all","conditions":[{"field":"city","op":"eq","value":"x"}]}`, "push", `unknown channel "push"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Build(parse(t, tt.rule), tt.channel)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("got %v, want ValidationError", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestBuildCollectsAllProblems(t *testing.T) {
	rule := `{"match":"all","conditions":[{"field":"nope","op":"eq","value":"x"},{"event":"nope","within_days":1,"exists":true}]}`
	_, _, err := Build(parse(t, rule), "sms")
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 2 {
		t.Fatalf("want 2 problems, got %v", err)
	}
}

func TestDepthLimit(t *testing.T) {
	leaf := `{"field":"city","op":"eq","value":"x"}`
	nest := func(levels int) string {
		s := leaf
		for i := 0; i < levels; i++ {
			s = `{"match":"all","conditions":[` + s + `]}`
		}
		return s
	}
	if _, _, err := Build(parse(t, nest(3)), "sms"); err != nil {
		t.Errorf("depth 3 should pass: %v", err)
	}
	if _, _, err := Build(parse(t, nest(4)), "sms"); err == nil || !strings.Contains(err.Error(), "deeper than 3") {
		t.Errorf("depth 4 should fail, got %v", err)
	}
}

func TestConditionLimit(t *testing.T) {
	conds := func(n int) string {
		return strings.TrimSuffix(strings.Repeat(`{"field":"city","op":"eq","value":"x"},`, n), ",")
	}
	if _, _, err := Build(parse(t, `{"match":"any","conditions":[`+conds(20)+`]}`), "sms"); err != nil {
		t.Errorf("20 conditions should pass: %v", err)
	}
	if _, _, err := Build(parse(t, `{"match":"any","conditions":[`+conds(21)+`]}`), "sms"); err == nil || !strings.Contains(err.Error(), "max 20") {
		t.Errorf("21 conditions should fail, got %v", err)
	}
}

// Hostile values must only ever travel as bound arguments, never inside the SQL.
func TestInjectionStaysInArgs(t *testing.T) {
	payload := "'; DROP TABLE users; --"
	rule := fmt.Sprintf(`{"match":"all","conditions":[{"field":"city","op":"eq","value":%q},{"field":"city","op":"in","value":[%q]}]}`, payload, payload)
	sql, args, err := Build(parse(t, rule), "sms")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "DROP") || strings.Contains(sql, "'") {
		t.Errorf("payload leaked into sql: %s", sql)
	}
	if args[0] != payload {
		t.Errorf("payload should be arg 1, got %#v", args)
	}
}

// Hostile names must be rejected by the whitelists, not interpolated.
func TestInjectionInNamesRejected(t *testing.T) {
	for _, rule := range []string{
		`{"match":"all","conditions":[{"field":"city; DROP TABLE users","op":"eq","value":"x"}]}`,
		`{"match":"all","conditions":[{"field":"city","op":"= 1 OR 1=1 --","value":"x"}]}`,
		`{"match":"all","conditions":[{"event":"purchase' OR '1'='1","within_days":7,"exists":true}]}`,
	} {
		if _, _, err := Build(parse(t, rule), "sms"); err == nil {
			t.Errorf("should be rejected: %s", rule)
		}
	}
}
