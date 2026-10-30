package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/praganter/insider-case/backend/internal/content"
)

type ContentRequest struct {
	Brief string
	// Slots are the messages to write: one per channel, variant and language.
	Slots []content.Slot
	// Set on a repair round: the rejected output and what was wrong with it.
	Previous json.RawMessage
	Problems []string
}

// Stream receives the raw model output as it arrives. Reset means the answer
// is being redone (retry or repair), so a preview built from deltas must restart.
type Stream struct {
	Delta func(string)
	Reset func()
}

func (s Stream) delta(x string) {
	if s.Delta != nil {
		s.Delta(x)
	}
}

func (s Stream) reset() {
	if s.Reset != nil {
		s.Reset()
	}
}

// InvalidContentError means the output was unusable (bad JSON, missing
// channel) even after the repair round. Limit violations are not an error.
type InvalidContentError struct {
	Raw      json.RawMessage
	Problems []string
}

func (e *InvalidContentError) Error() string { return "llm content invalid after repair" }

// BuildContent generates copy, checks it with check (limits, placeholders) and
// allows one repair round. Content that still breaks limits after the repair is
// returned with its problems, so the user can edit it; nothing is sent anyway
// until approval, and approval re-checks.
func BuildContent(ctx context.Context, c Client, req ContentRequest, s Stream, check func([]content.Message) []string) ([]content.Message, []string, error) {
	raw, err := c.Content(ctx, req, s)
	if err != nil {
		return nil, nil, err
	}
	msgs, problems := parseMessages(raw, req.Slots)
	if len(problems) == 0 {
		problems = check(msgs)
	}
	if len(problems) == 0 {
		return msgs, nil, nil
	}

	req.Previous, req.Problems = raw, problems
	s.reset()
	raw, err = c.Content(ctx, req, s)
	if err != nil {
		return nil, nil, err
	}
	msgs, shape := parseMessages(raw, req.Slots)
	if len(shape) > 0 {
		return nil, nil, &InvalidContentError{Raw: raw, Problems: shape}
	}
	return msgs, check(msgs), nil
}

func parseMessages(raw json.RawMessage, slots []content.Slot) ([]content.Message, []string) {
	var out struct {
		Messages []content.Message `json:"messages"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, []string{"invalid JSON: " + err.Error()}
	}
	var problems []string
	seen := map[content.Slot]int{}
	for _, m := range out.Messages {
		seen[m.Slot()]++
	}
	for _, sl := range slots {
		if seen[sl] != 1 {
			problems = append(problems, fmt.Sprintf("need exactly one %s message for variant %s in %s, got %d", sl.Channel, sl.Variant, sl.Language, seen[sl]))
		}
		delete(seen, sl)
	}
	for sl := range seen {
		problems = append(problems, fmt.Sprintf("unexpected message %s/%s/%s", sl.Channel, sl.Variant, sl.Language))
	}
	return out.Messages, problems
}

func repairNote(previous json.RawMessage, problems []string) string {
	if previous == nil {
		return ""
	}
	return "\n\nYour previous answer was rejected:\n" + string(previous) +
		"\n\nProblems:\n- " + strings.Join(problems, "\n- ") + "\n\nReturn a corrected answer."
}
