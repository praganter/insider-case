package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const validRule = `{"match":"all","conditions":[{"field":"city","op":"eq","value":"Ankara"}]}`

// reply is one canned Messages API response.
type reply struct {
	status  int
	stop    string
	text    string
	details string // raw JSON for stop_details, optional
	headers map[string]string
}

func message(r reply) string {
	if r.stop == "" {
		r.stop = "end_turn"
	}
	details := "null"
	if r.details != "" {
		details = r.details
	}
	text, _ := json.Marshal(r.text)
	return fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":%s}],`+
		`"stop_reason":%q,"stop_details":%s,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`, text, r.stop, details)
}

// fakeAPI serves replies in order (the last one repeats) and records request bodies.
func fakeAPI(t *testing.T, replies ...reply) (*Anthropic, *[]string, *atomic.Int32) {
	t.Helper()
	var bodies []string
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		i := int(n.Add(1)) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		rep := replies[i]
		for k, v := range rep.headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		if rep.status != 0 {
			w.WriteHeader(rep.status)
			fmt.Fprint(w, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)
			return
		}
		if strings.Contains(string(b), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sse(rep))
			return
		}
		fmt.Fprint(w, message(rep))
	}))
	t.Cleanup(srv.Close)
	return NewAnthropic("test-key", "claude-test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0)), &bodies, &n
}

func TestAnthropicHappyPath(t *testing.T) {
	a, bodies, _ := fakeAPI(t, reply{text: validRule})
	node, err := BuildAudience(context.Background(), a, "Ankara'daki kullanıcılar")
	if err != nil {
		t.Fatal(err)
	}
	if node.Conditions[0].Value != "Ankara" {
		t.Errorf("got %+v", node)
	}
	body := (*bodies)[0]
	for _, want := range []string{`"model":"claude-test"`, `"output_config"`, `"json_schema"`, `"$defs"`, `Ankara'daki`, "Today is"} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %s", want)
		}
	}
	if strings.Contains(body, "tool_choice") {
		t.Error("forced tool_choice must not be sent")
	}
}

func TestAnthropicRepairSendsProblems(t *testing.T) {
	bad := `{"match":"all","conditions":[{"field":"password","op":"eq","value":"x"}]}`
	a, bodies, n := fakeAPI(t, reply{text: bad}, reply{text: validRule})
	if _, err := BuildAudience(context.Background(), a, "x"); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("want 2 requests, got %d", n.Load())
	}
	if !strings.Contains((*bodies)[1], `unknown field`) || !strings.Contains((*bodies)[1], "password") {
		t.Errorf("repair request should carry the previous rule and problems: %s", (*bodies)[1])
	}
}

func TestAnthropicRefusal(t *testing.T) {
	a, _, _ := fakeAPI(t, reply{stop: "refusal", details: `{"type":"refusal","category":"cyber","explanation":""}`})
	_, err := BuildAudience(context.Background(), a, "x")
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestAnthropicMaxTokensRetriesOnce(t *testing.T) {
	a, _, n := fakeAPI(t, reply{stop: "max_tokens", text: `{"match":"al`}, reply{text: validRule})
	if _, err := BuildAudience(context.Background(), a, "x"); err != nil {
		t.Fatalf("second attempt should succeed: %v", err)
	}
	if n.Load() != 2 {
		t.Errorf("want 2 requests, got %d", n.Load())
	}

	a, _, n = fakeAPI(t, reply{stop: "max_tokens", text: `{"match":"al`})
	_, err := BuildAudience(context.Background(), a, "x")
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("got %v", err)
	}
	if n.Load() != 2 {
		t.Errorf("want exactly 2 requests, got %d", n.Load())
	}
}

func TestAnthropicAPIErrorsAreUnavailable(t *testing.T) {
	for _, status := range []int{429, 500, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a, _, _ := fakeAPI(t, reply{status: status})
			_, err := BuildAudience(context.Background(), a, "x")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// The SDK's own retry should ride out one transient 529.
func TestAnthropicSDKRetriesTransientOverload(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if n.Add(1) == 1 {
			w.Header().Set("retry-after-ms", "1")
			w.WriteHeader(529)
			fmt.Fprint(w, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)
			return
		}
		fmt.Fprint(w, message(reply{text: validRule}))
	}))
	defer srv.Close()
	a := NewAnthropic("k", "m", option.WithBaseURL(srv.URL))
	if _, err := BuildAudience(context.Background(), a, "x"); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Errorf("want 2 requests, got %d", n.Load())
	}
}

func TestAnthropicNeverFallsBackToMock(t *testing.T) {
	a, _, _ := fakeAPI(t, reply{status: 500})
	if a.Mode() != "anthropic" {
		t.Fatal("mode must stay anthropic")
	}
	if _, err := BuildAudience(context.Background(), a, "x"); err == nil {
		t.Fatal("API failure must surface as an error, not a mock answer")
	}
}

func TestAudienceSchemaShape(t *testing.T) {
	b, err := json.Marshal(audienceSchema())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"additionalProperties":false`, `"cart_add"`, `"within_days"`, `"group1"`, `"group2"`} {
		if !strings.Contains(s, want) {
			t.Errorf("schema missing %s", want)
		}
	}
	// Structured outputs reject these keywords.
	for _, bad := range []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"} {
		if strings.Contains(s, `"`+bad+`"`) {
			t.Errorf("schema uses unsupported keyword %s", bad)
		}
	}
}

// sse renders a reply as a Messages API event stream, the text split in three pieces.
func sse(r reply) string {
	if r.stop == "" {
		r.stop = "end_turn"
	}
	details := ""
	if r.details != "" {
		details = `,"stop_details":` + r.details
	}
	event := func(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }
	out := event("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`)
	out += event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	rs := []rune(r.text)
	for _, part := range []string{string(rs[:len(rs)/3]), string(rs[len(rs)/3 : 2*len(rs)/3]), string(rs[2*len(rs)/3:])} {
		p, _ := json.Marshal(part)
		out += event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":`+string(p)+`}}`)
	}
	out += event("content_block_stop", `{"type":"content_block_stop","index":0}`)
	out += event("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null%s},"usage":{"output_tokens":5}}`, r.stop, details))
	return out + event("message_stop", `{"type":"message_stop"}`)
}

const validContent = `{"messages":[{"channel":"sms","variant":"A","language":"tr","subject":"","body":"Merhaba {{first_name}}"}]}`

func contentReq() ContentRequest {
	return ContentRequest{Brief: "Ankara kampanyası", Slots: slots("sms")}
}

func TestAnthropicContentStreams(t *testing.T) {
	a, bodies, _ := fakeAPI(t, reply{text: validContent})
	var deltas []string
	raw, err := a.Content(context.Background(), contentReq(), Stream{Delta: func(d string) { deltas = append(deltas, d) }})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != validContent || strings.Join(deltas, "") != validContent || len(deltas) != 3 {
		t.Errorf("raw=%s deltas=%q", raw, deltas)
	}
	body := (*bodies)[0]
	for _, want := range []string{`"stream":true`, `"output_config"`, `"json_schema"`, "Ankara kampanyası", `{{first_name}}`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %s", want)
		}
	}
	if strings.Contains(body, "tool_choice") {
		t.Error("forced tool_choice must not be sent")
	}
}

func TestAnthropicContentRefusal(t *testing.T) {
	a, _, _ := fakeAPI(t, reply{stop: "refusal", text: "no", details: `{"type":"refusal","category":"cyber","explanation":""}`})
	if _, err := a.Content(context.Background(), contentReq(), Stream{}); !errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestAnthropicContentMaxTokensRetriesWithReset(t *testing.T) {
	a, _, n := fakeAPI(t, reply{stop: "max_tokens", text: `{"messages":[{"chan`}, reply{text: validContent})
	var resets int
	raw, err := a.Content(context.Background(), contentReq(), Stream{Reset: func() { resets++ }})
	if err != nil || string(raw) != validContent {
		t.Fatalf("raw=%s err=%v", raw, err)
	}
	if n.Load() != 2 || resets != 1 {
		t.Errorf("requests=%d resets=%d", n.Load(), resets)
	}

	a, _, _ = fakeAPI(t, reply{stop: "max_tokens", text: `{"messages":[{"chan`})
	if _, err := a.Content(context.Background(), contentReq(), Stream{}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("got %v", err)
	}
}

func TestAnthropicContentAPIErrors(t *testing.T) {
	for _, status := range []int{429, 500, 529} {
		a, _, _ := fakeAPI(t, reply{status: status})
		if _, err := a.Content(context.Background(), contentReq(), Stream{}); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%d: got %v", status, err)
		}
	}
}

func TestAnthropicContentRepairSendsProblems(t *testing.T) {
	long := `{"messages":[{"channel":"sms","variant":"A","language":"tr","subject":"","body":"` + strings.Repeat("x", 170) + `"}]}`
	a, bodies, _ := fakeAPI(t, reply{text: long}, reply{text: validContent})
	msgs, problems, err := BuildContent(context.Background(), a, contentReq(), Stream{}, limitCheck)
	if err != nil || len(problems) != 0 || len(msgs) != 1 {
		t.Fatalf("msgs=%v problems=%v err=%v", msgs, problems, err)
	}
	if !strings.Contains((*bodies)[1], "max 160") {
		t.Errorf("repair request should carry the limit problem: %s", (*bodies)[1])
	}
}

func TestContentSchemaShape(t *testing.T) {
	b, _ := json.Marshal(contentSchema())
	for _, want := range []string{`"additionalProperties":false`, `"messages"`, `"enum":["sms","email"]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("schema missing %s", want)
		}
	}
}
