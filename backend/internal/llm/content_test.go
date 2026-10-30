package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/praganter/insider-case/backend/internal/content"
)

func noViolations([]content.Message) []string { return nil }

func limitCheck(msgs []content.Message) []string {
	var p []string
	for _, m := range msgs {
		p = append(p, content.Check(m, map[string]string{"first_name": "Ayşenur", "last_name": "Yıldırımoğlu", "city": "Gaziantep"})...)
	}
	return p
}

func TestMockContentIsAlwaysWithinLimits(t *testing.T) {
	briefs := []string{
		"İstanbul'daki sepeti terk edenlere %20 indirim, samimi bir dille",
		"Sayın müşterilerimize resmi bir dille %15 indirim duyurusu",
		"Thank customers who bought in the last 7 days, 10% discount",
		"hello",
		"",
	}
	for _, b := range briefs {
		msgs, problems, err := BuildContent(context.Background(), Mock{}, ContentRequest{Brief: b, Slots: slots("sms", "email")}, Stream{}, limitCheck)
		if err != nil || len(problems) > 0 {
			t.Fatalf("%q: err=%v problems=%v", b, err, problems)
		}
		if len(msgs) != 2 || utf8.RuneCountInString(msgs[0].Body) > content.SMSMax {
			t.Errorf("%q: bad messages %+v", b, msgs)
		}
	}
}

func TestMockContentFollowsBrief(t *testing.T) {
	get := func(brief string) []content.Message {
		msgs, _, err := BuildContent(context.Background(), Mock{}, ContentRequest{Brief: brief, Slots: slots("sms", "email")}, Stream{}, noViolations)
		if err != nil {
			t.Fatal(err)
		}
		return msgs
	}
	tr := get("Sepeti terk edenlere %20 indirim, samimi")
	if !strings.Contains(tr[0].Body, "Merhaba {{first_name}}") || !strings.Contains(tr[0].Body, "%20 indirim") || !strings.Contains(tr[0].Body, "sepet") {
		t.Errorf("turkish casual cart sms: %q", tr[0].Body)
	}
	formal := get("resmi bir dille duyuru")
	if !strings.Contains(formal[0].Body, "Sayın {{first_name}} {{last_name}}") {
		t.Errorf("formal tone: %q", formal[0].Body)
	}
	enMsgs, _, err := BuildContent(context.Background(), Mock{}, ContentRequest{Brief: "Customers with a cart, 10% discount",
		Slots: []content.Slot{{Channel: "sms", Variant: "A", Language: "en"}, {Channel: "email", Variant: "A", Language: "en"}}}, Stream{}, noViolations)
	if err != nil {
		t.Fatal(err)
	}
	en := enMsgs
	if !strings.Contains(en[0].Body, "Hi {{first_name}}") || !strings.Contains(en[0].Body, "10% off") {
		t.Errorf("english: %q", en[0].Body)
	}
	if en[1].Subject == "" || en[1].Channel != "email" {
		t.Errorf("email: %+v", en[1])
	}
}

func TestMockOnlyRequestedChannels(t *testing.T) {
	msgs, _, err := BuildContent(context.Background(), Mock{}, ContentRequest{Brief: "x", Slots: slots("email")}, Stream{}, noViolations)
	if err != nil || len(msgs) != 1 || msgs[0].Channel != "email" {
		t.Fatalf("got %+v, %v", msgs, err)
	}
}

func TestMockStreamsInPieces(t *testing.T) {
	var deltas []string
	var resets int
	s := Stream{Delta: func(d string) { deltas = append(deltas, d) }, Reset: func() { resets++ }}
	raw, err := Mock{}.Content(context.Background(), ContentRequest{Brief: "x", Slots: slots("sms")}, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) < 3 || strings.Join(deltas, "") != string(raw) || resets != 0 {
		t.Errorf("deltas=%d joined-equal=%v resets=%d", len(deltas), strings.Join(deltas, "") == string(raw), resets)
	}
}

func TestMockStreamStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Mock{Delay: 1}.Content(ctx, ContentRequest{Brief: "x", Slots: slots("sms")}, Stream{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

// First answer has a 170-character SMS: the repair round fixes it and the UI
// preview is told to restart.
func TestContentRepairOnLimitViolation(t *testing.T) {
	var resets int
	msgs, problems, err := BuildContent(context.Background(), Mock{BadFirst: true},
		ContentRequest{Brief: "x", Slots: slots("sms")}, Stream{Reset: func() { resets++ }}, limitCheck)
	if err != nil || len(problems) > 0 {
		t.Fatalf("err=%v problems=%v", err, problems)
	}
	if resets != 1 || utf8.RuneCountInString(msgs[0].Body) > content.SMSMax {
		t.Errorf("resets=%d msgs=%+v", resets, msgs)
	}
}

// If the model keeps breaking a limit, the user still gets the text plus the
// problems, so they can edit it.
func TestContentViolationsAfterRepairAreReturned(t *testing.T) {
	c := &stubClient{rule: `{"messages":[{"channel":"sms","variant":"A","language":"tr","subject":"","body":"` + strings.Repeat("x", 170) + `"}]}`}
	msgs, problems, err := BuildContent(context.Background(), c, ContentRequest{Brief: "x", Slots: slots("sms")}, Stream{}, limitCheck)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || len(problems) != 1 || !strings.Contains(problems[0], "170") {
		t.Errorf("msgs=%+v problems=%v", msgs, problems)
	}
	if c.calls != 2 {
		t.Errorf("want exactly one repair round, got %d calls", c.calls)
	}
}

func TestContentShapeProblems(t *testing.T) {
	tests := []struct{ name, raw string }{
		{"not json", `nope`},
		{"missing channel", `{"messages":[{"channel":"sms","variant":"A","language":"tr","subject":"","body":"hi"}]}`},
		{"duplicate channel", `{"messages":[{"channel":"sms","variant":"A","language":"tr","subject":"","body":"a"},{"channel":"sms","variant":"A","language":"tr","subject":"","body":"b"},{"channel":"email","variant":"A","language":"tr","subject":"s","body":"b"}]}`},
		{"extra channel", `{"messages":[{"channel":"sms","variant":"A","language":"tr","subject":"","body":"a"},{"channel":"email","variant":"A","language":"tr","subject":"s","body":"b"},{"channel":"push","variant":"A","language":"tr","subject":"","body":"c"}]}`},
		{"unknown key", `{"messages":[],"note":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &stubClient{rule: tt.raw}
			_, _, err := BuildContent(context.Background(), c, ContentRequest{Brief: "x", Slots: slots("sms", "email")}, Stream{}, noViolations)
			var ice *InvalidContentError
			if !errors.As(err, &ice) || len(ice.Problems) == 0 {
				t.Fatalf("want InvalidContentError, got %v", err)
			}
			if c.calls != 2 {
				t.Errorf("want 2 calls, got %d", c.calls)
			}
		})
	}
}

func TestContentErrorPassesThrough(t *testing.T) {
	_, _, err := BuildContent(context.Background(), Mock{Err: ErrRefused}, ContentRequest{Slots: slots("sms")}, Stream{}, noViolations)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
}

// slots is one Turkish variant-A slot per channel.
func slots(channels ...string) []content.Slot {
	var out []content.Slot
	for _, ch := range channels {
		out = append(out, content.Slot{Channel: ch, Variant: "A", Language: "tr"})
	}
	return out
}

// One brief, two languages and two variants: the mock follows the slots, and
// variant B differs from A.
func TestMockContentPerLanguageAndVariant(t *testing.T) {
	var want []content.Slot
	for _, l := range []string{"tr", "en"} {
		for _, v := range []string{"A", "B"} {
			want = append(want, content.Slot{Channel: "sms", Variant: v, Language: l})
		}
	}
	msgs, problems, err := BuildContent(context.Background(), Mock{}, ContentRequest{Brief: "Sepeti terk edenlere %20 indirim", Slots: want}, Stream{}, limitCheck)
	if err != nil || len(problems) > 0 || len(msgs) != 4 {
		t.Fatalf("msgs=%+v problems=%v err=%v", msgs, problems, err)
	}
	if !strings.Contains(msgs[0].Body, "Merhaba") || !strings.Contains(msgs[2].Body, "Hi {{first_name}}") || msgs[0].Body == msgs[1].Body {
		t.Errorf("language or variant not followed: %+v", msgs)
	}
	// A slot left out is a shape error, not a silently missing translation.
	c := &stubClient{rule: `{"messages":[{"channel":"sms","variant":"A","language":"tr","subject":"","body":"x"}]}`}
	_, _, err = BuildContent(context.Background(), c, ContentRequest{Brief: "x", Slots: want}, Stream{}, noViolations)
	var ice *InvalidContentError
	if !errors.As(err, &ice) {
		t.Errorf("missing slots: %v", err)
	}
}
