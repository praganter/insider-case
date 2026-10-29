package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/praganter/insider-case/backend/internal/content"
)

const contentTimeout = 90 * time.Second

func (a *Anthropic) Content(ctx context.Context, req ContentRequest, s Stream) (json.RawMessage, error) {
	user := "Brief:\n" + req.Brief + "\n\nChannels: " + fmt.Sprint(req.Channels) + repairNote(req.Previous, req.Problems)
	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 4000,
		System:    []anthropic.TextBlockParam{{Text: contentPrompt(a.now())}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: contentSchema()},
		},
	}
	for attempt := 0; ; attempt++ {
		stream := a.client.Messages.NewStreaming(ctx, params, option.WithRequestTimeout(contentTimeout))
		var msg anthropic.Message
		for stream.Next() {
			ev := stream.Current()
			if err := msg.Accumulate(ev); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
			}
			if d, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if t, ok := d.Delta.AsAny().(anthropic.TextDelta); ok {
					s.delta(t.Text)
				}
			}
		}
		if err := stream.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		text, retry, err := outcome(&msg, attempt)
		if !retry {
			return text, err
		}
		s.reset()
	}
}

func contentPrompt(now time.Time) string {
	return fmt.Sprintf(`You write marketing messages for a campaign tool. Today is %s.
Return only JSON matching the schema: one message per requested channel.

Rules:
- Write in the language of the brief (a Turkish brief gets Turkish copy). Follow the tone the brief asks for.
- SMS: body at most %d characters, subject must be an empty string. Email: subject at most %d characters, plus a body.
- Personalize only with these placeholders, written exactly like {{first_name}}: %s.
  The limits count characters AFTER the placeholders are filled in, and names can be 20 characters or more,
  so keep an SMS under about 140 characters in its template form.
- No other {{...}} text, no links or phone numbers you were not given.`,
		now.Format("2006-01-02"), content.SMSMax, content.SubjectMax, "first_name, last_name, city")
}

// contentSchema fixes the shape only; length limits are checked in Go because
// structured outputs do not support them.
func contentSchema() map[string]any {
	str := map[string]any{"type": "string"}
	msg := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{"type": "string", "enum": []string{"sms", "email"}},
			"subject": str,
			"body":    str,
		},
		"required":             []string{"channel", "subject", "body"},
		"additionalProperties": false,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"messages": map[string]any{"type": "array", "items": msg},
		},
		"required":             []string{"messages"},
		"additionalProperties": false,
	}
}
