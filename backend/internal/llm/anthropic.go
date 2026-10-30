package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/praganter/insider-case/backend/internal/segment"
)

type Anthropic struct {
	client anthropic.Client
	model  string
	now    func() time.Time
}

// NewAnthropic builds the real client. Extra options come last, so tests can
// point it at a fake server with option.WithBaseURL.
func NewAnthropic(apiKey, model string, opts ...option.RequestOption) *Anthropic {
	base := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(2),
		option.WithRequestTimeout(30 * time.Second),
	}
	return &Anthropic{client: anthropic.NewClient(append(base, opts...)...), model: model, now: time.Now}
}

func (*Anthropic) Mode() string { return "anthropic" }

func (a *Anthropic) Audience(ctx context.Context, brief string, previous json.RawMessage, problems []string) (json.RawMessage, error) {
	user := "Brief:\n" + brief
	if previous != nil {
		user += "\n\nYour previous rule was rejected:\n" + string(previous) +
			"\n\nProblems:\n- " + strings.Join(problems, "\n- ") + "\n\nReturn a corrected rule."
	}
	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 2000,
		System:    []anthropic.TextBlockParam{{Text: audiencePrompt(a.now())}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: audienceSchema()},
		},
	}
	for attempt := 0; ; attempt++ {
		resp, err := a.client.Messages.New(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if text, retry, err := outcome(resp, attempt); !retry {
			return text, err
		}
	}
}

// outcome turns a finished response into text or the matching error. retry is
// true once when the output was cut off by max_tokens.
func outcome(resp *anthropic.Message, attempt int) (text json.RawMessage, retry bool, err error) {
	switch resp.StopReason {
	case anthropic.StopReasonRefusal:
		log.Printf("llm: refusal, category=%q", resp.StopDetails.Category)
		return nil, false, ErrRefused
	case anthropic.StopReasonMaxTokens:
		if attempt == 0 {
			return nil, true, nil
		}
		return nil, false, ErrTruncated
	}
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			return json.RawMessage(t.Text), false, nil
		}
	}
	return nil, false, fmt.Errorf("%w: response had no text", ErrUnavailable)
}

func audiencePrompt(now time.Time) string {
	fields, ops, events := segment.Whitelist()
	return fmt.Sprintf(`You turn a marketing brief into an audience rule for a campaign tool. Today is %s.
Return only the rule, as JSON matching the schema.

A rule is a group: {"match":"all"|"any","conditions":[...]}. A condition is a field condition
{"field","op","value"}, an event condition {"event","within_days","exists"}, or a nested group.
Nest at most 3 levels and use at most %d conditions in total.

Fields: %s. city, language and gender take text (ops eq, neq, in); age takes a whole number
(eq, neq, in, gt, gte, lt, lte); created_at takes a date like 2025-01-31 (gt, gte, lt, lte).
language values are "tr" or "en". gender values are exactly %s. "in" takes a list of at most %d items.
Events: %s, with within_days 1..%d. exists=false means the user did NOT do it in that window.
Allowed ops: %s.
Use only these names. Never write SQL. Use the exact city spelling from the brief (e.g. İstanbul).`,
		now.Format("2006-01-02"), segment.MaxConditions,
		strings.Join(fields, ", "), `"`+strings.Join(segment.Values("gender"), `" or "`)+`"`, segment.MaxInValues,
		strings.Join(events, ", "), segment.MaxWithinDays, strings.Join(ops, ", "))
}

// audienceSchema fixes the JSON shape only (structured outputs cannot express
// recursion or limits); segment.Build enforces the rules. Depth 3 is unrolled:
// group3 (root) -> group2 -> group1 -> leaves.
func audienceSchema() map[string]any {
	fields, ops, events := segment.Whitelist()
	obj := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	enum := func(vals []string) map[string]any { return map[string]any{"type": "string", "enum": vals} }
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/$defs/" + name} }
	group := func(children ...map[string]any) map[string]any {
		return obj(map[string]any{
			"match":      enum([]string{"all", "any"}),
			"conditions": map[string]any{"type": "array", "items": map[string]any{"anyOf": children}},
		}, "match", "conditions")
	}
	value := map[string]any{"anyOf": []any{
		map[string]any{"type": "string"},
		map[string]any{"type": "number"},
		map[string]any{"type": "array", "items": map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}}}},
	}}
	schema := group(ref("fieldCond"), ref("eventCond"), ref("group2"))
	schema["$defs"] = map[string]any{
		"fieldCond": obj(map[string]any{"field": enum(fields), "op": enum(ops), "value": value}, "field", "op", "value"),
		"eventCond": obj(map[string]any{
			"event":       enum(events),
			"within_days": map[string]any{"type": "integer"},
			"exists":      map[string]any{"type": "boolean"},
		}, "event", "within_days", "exists"),
		"group2": group(ref("fieldCond"), ref("eventCond"), ref("group1")),
		"group1": group(ref("fieldCond"), ref("eventCond")),
	}
	return schema
}
