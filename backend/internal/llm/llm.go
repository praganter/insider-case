// Package llm turns a marketer's brief into an audience rule. All access goes
// through Client, so tests and key-less runs use the mock instead of the API.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/praganter/insider-case/backend/internal/segment"
)

var (
	// ErrRefused: the model declined the brief; rewording is the only fix.
	ErrRefused = errors.New("llm refused the brief")
	// ErrTruncated: output hit max_tokens even after one retry.
	ErrTruncated = errors.New("llm output was cut off")
	// ErrUnavailable: API/network failure; the user can retry.
	ErrUnavailable = errors.New("llm unavailable")
)

// InvalidRuleError means the rule stayed invalid after the repair round.
type InvalidRuleError struct {
	Rule     json.RawMessage
	Problems []string
}

func (e *InvalidRuleError) Error() string { return "llm rule invalid after repair" }

type Client interface {
	// Audience returns a rule as JSON. On a repair round, previous is the
	// rejected rule and problems are the validator's complaints.
	Audience(ctx context.Context, brief string, previous json.RawMessage, problems []string) (json.RawMessage, error)
	// Mode is "mock" or "anthropic", shown in the UI.
	Mode() string
}

// BuildAudience asks for a rule, validates it, and allows exactly one repair round.
func BuildAudience(ctx context.Context, c Client, brief string) (segment.Node, error) {
	raw, err := c.Audience(ctx, brief, nil, nil)
	if err != nil {
		return segment.Node{}, err
	}
	node, problems := check(raw)
	if len(problems) == 0 {
		return node, nil
	}
	raw, err = c.Audience(ctx, brief, raw, problems)
	if err != nil {
		return segment.Node{}, err
	}
	node, problems = check(raw)
	if len(problems) > 0 {
		return segment.Node{}, &InvalidRuleError{Rule: raw, Problems: problems}
	}
	return node, nil
}

func check(raw json.RawMessage) (segment.Node, []string) {
	var n segment.Node
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&n); err != nil {
		return n, []string{"invalid JSON: " + err.Error()}
	}
	if _, _, err := segment.Build(n, "sms"); err != nil {
		var ve *segment.ValidationError
		if errors.As(err, &ve) {
			return n, ve.Problems
		}
		return n, []string{fmt.Sprint(err)}
	}
	return n, nil
}
