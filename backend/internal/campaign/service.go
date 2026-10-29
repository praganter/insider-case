package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/praganter/insider-case/backend/internal/content"
	"github.com/praganter/insider-case/backend/internal/llm"
	"github.com/praganter/insider-case/backend/internal/segment"
	"github.com/praganter/insider-case/backend/internal/store"
)

// InputError is a bad request (400); LimitError is a valid request that the
// content limits or an empty audience forbid (422).
type InputError struct{ Problems []string }

func (e *InputError) Error() string { return strings.Join(e.Problems, "; ") }

type LimitError struct{ Problems []string }

func (e *LimitError) Error() string { return strings.Join(e.Problems, "; ") }

type Service struct {
	Store *store.Store
	LLM   llm.Client
}

type View struct {
	store.Campaign
	Contents []content.Message `json:"contents"`
}

func (s *Service) Get(ctx context.Context, id int64) (View, error) {
	c, err := s.Store.GetCampaign(ctx, id)
	if err != nil {
		return View{}, err
	}
	msgs, err := s.Store.GetContents(ctx, id)
	return View{c, msgs}, err
}

func (s *Service) Create(ctx context.Context, brief string, channels []string) (store.Campaign, error) {
	var problems []string
	if strings.TrimSpace(brief) == "" {
		problems = append(problems, "brief is empty")
	}
	if len(channels) == 0 {
		problems = append(problems, "pick at least one channel")
	}
	seen := map[string]bool{}
	for _, ch := range channels {
		if ch != "sms" && ch != "email" || seen[ch] {
			problems = append(problems, fmt.Sprintf("channel %q is unknown or repeated", ch))
		}
		seen[ch] = true
	}
	if len(problems) > 0 {
		return store.Campaign{}, &InputError{problems}
	}
	return s.Store.CreateCampaign(ctx, brief, channels)
}

// SetAudience validates the rule for every channel of the campaign and stores
// it. The returned counts are per channel (consent applies per channel).
func (s *Service) SetAudience(ctx context.Context, id int64, rule segment.Node) (map[string]int, error) {
	c, err := s.Store.GetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	if !CanMove(c.Status, AudienceReady) {
		return nil, &ConflictError{c.Status, AudienceReady}
	}
	stats, err := s.audience(ctx, c, rule)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(rule)
	if err := s.Store.SetAudience(ctx, id, c.Status, raw); err != nil {
		return nil, conflict(err, c.Status, AudienceReady)
	}
	counts := map[string]int{}
	for ch, a := range stats {
		counts[ch] = a.Count
	}
	return counts, nil
}

func (s *Service) audience(ctx context.Context, c store.Campaign, rule segment.Node) (map[string]store.Audience, error) {
	out := map[string]store.Audience{}
	for _, ch := range c.Channels {
		where, args, err := segment.Build(rule, ch)
		var ve *segment.ValidationError
		if errors.As(err, &ve) {
			return nil, &InputError{ve.Problems}
		}
		if out[ch], err = s.Store.AudienceStats(ctx, where, args); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// worstCase renders each message with the longest first name, last name and
// city of its audience, so approval cannot silently lose anyone to a limit.
func worstCase(stats map[string]store.Audience) func([]content.Message) []string {
	return func(msgs []content.Message) []string {
		var problems []string
		for _, m := range msgs {
			problems = append(problems, content.Check(m, stats[m.Channel].Longest)...)
		}
		return problems
	}
}

func (s *Service) withAudience(ctx context.Context, c store.Campaign) (map[string]store.Audience, error) {
	var rule segment.Node
	if err := json.Unmarshal(c.Rule, &rule); err != nil {
		return nil, fmt.Errorf("campaign %d has no valid audience rule: %w", c.ID, err)
	}
	return s.audience(ctx, c, rule)
}

// Result is the generated or edited copy plus any limit violations still left.
type Result struct {
	Messages []content.Message `json:"messages"`
	Problems []string          `json:"problems"`
}

// GenerateContent asks the LLM for copy and stores it. Limit violations that
// survive the repair round are returned for the user to fix, not hidden.
func (s *Service) GenerateContent(ctx context.Context, id int64, stream llm.Stream) (Result, error) {
	c, err := s.Store.GetCampaign(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if !CanMove(c.Status, ContentReady) {
		return Result{}, &ConflictError{c.Status, ContentReady}
	}
	stats, err := s.withAudience(ctx, c)
	if err != nil {
		return Result{}, err
	}
	msgs, problems, err := llm.BuildContent(ctx, s.LLM,
		llm.ContentRequest{Brief: c.Brief, Channels: c.Channels}, stream, worstCase(stats))
	if err != nil {
		return Result{}, err
	}
	if err := s.Store.SaveContents(ctx, id, c.Status, msgs); err != nil {
		return Result{}, conflict(err, c.Status, ContentReady)
	}
	return Result{msgs, problems}, nil
}

// EditContent saves the user's copy as given and reports what still breaks a
// limit; approval stays blocked until it is clean.
func (s *Service) EditContent(ctx context.Context, id int64, msgs []content.Message) (Result, error) {
	c, err := s.Store.GetCampaign(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if c.Status != ContentReady {
		return Result{}, &ConflictError{c.Status, ContentReady}
	}
	stats, err := s.withAudience(ctx, c)
	if err != nil {
		return Result{}, err
	}
	if err := sameChannels(c.Channels, msgs); err != nil {
		return Result{}, err
	}
	if err := s.Store.SaveContents(ctx, id, c.Status, msgs); err != nil {
		return Result{}, conflict(err, c.Status, ContentReady)
	}
	return Result{msgs, worstCase(stats)(msgs)}, nil
}

// Approve is the only way to Approved. It re-checks the stored copy against
// the audience as it is now, so nothing over a limit gets approved.
func (s *Service) Approve(ctx context.Context, id int64) (time.Time, error) {
	c, err := s.Store.GetCampaign(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if !CanMove(c.Status, Approved) {
		return time.Time{}, &ConflictError{c.Status, Approved}
	}
	stats, err := s.withAudience(ctx, c)
	if err != nil {
		return time.Time{}, err
	}
	var problems []string
	for _, ch := range c.Channels {
		if stats[ch].Count == 0 {
			problems = append(problems, ch+": the audience is empty")
		}
	}
	msgs, err := s.Store.GetContents(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if err := sameChannels(c.Channels, msgs); err != nil {
		return time.Time{}, err
	}
	problems = append(problems, worstCase(stats)(msgs)...)
	if len(problems) > 0 {
		return time.Time{}, &LimitError{problems}
	}
	if err := s.Store.Approve(ctx, id); err != nil {
		return time.Time{}, conflict(err, c.Status, Approved)
	}
	return time.Now(), nil
}

func sameChannels(channels []string, msgs []content.Message) error {
	have := map[string]int{}
	for _, m := range msgs {
		have[m.Channel]++
	}
	var problems []string
	for _, ch := range channels {
		if have[ch] != 1 {
			problems = append(problems, fmt.Sprintf("need exactly one %s message, got %d", ch, have[ch]))
		}
		delete(have, ch)
	}
	for ch := range have {
		problems = append(problems, fmt.Sprintf("campaign has no %s channel", ch))
	}
	if len(problems) > 0 {
		return &InputError{problems}
	}
	return nil
}

func conflict(err error, from, to string) error {
	if errors.Is(err, store.ErrStateChanged) {
		return &ConflictError{from, to}
	}
	return err
}
