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
