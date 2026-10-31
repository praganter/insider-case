package campaign

import (
	"context"
	"strings"
	"testing"

	"github.com/praganter/insider-case/backend/internal/content"
	"github.com/praganter/insider-case/backend/internal/llm"
	"github.com/praganter/insider-case/backend/internal/segment"
)

// Ankara speaks Turkish (one user has a 40-letter name), Bursa English.
func ankaraAndBursa() segment.Node {
	return segment.Node{Match: "any", Conditions: []segment.Node{
		{Field: "city", Op: "eq", Value: "Ankara"}, {Field: "city", Op: "eq", Value: "Bursa"}}}
}

func TestABTestAndLanguagesGiveOneMessagePerSlotAndSplitTheAudience(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	s.Enqueue = func(context.Context, int64, string) error { return nil }
	c, _ := s.Create(ctx, "%10 indirim", []string{"sms"}, true)
	if _, err := s.SetAudience(ctx, c.ID, ankaraAndBursa()); err != nil {
		t.Fatal(err)
	}
	res, err := s.GenerateContent(ctx, c.ID, llm.Stream{})
	if err != nil || len(res.Problems) != 0 || len(res.Messages) != 4 {
		t.Fatalf("want tr and en copy for variants A and B: %+v %v", res, err)
	}
	if _, err := s.Approve(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(ctx, c.ID)
	a, b := v.Variants["A"]["pending"], v.Variants["B"]["pending"]
	if a == 0 || b == 0 || a+b != 42 || len(v.Variants) != 2 {
		t.Errorf("variants %v, want both used and 42 messages (2 Ankara SMS users + 40 Bursa)", v.Variants)
	}
}

func TestWithoutABTestEveryoneGetsVariantA(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	s.Enqueue = func(context.Context, int64, string) error { return nil }
	c, _ := s.Create(ctx, "x", []string{"sms"}, false)
	s.SetAudience(ctx, c.ID, ankaraAndBursa())
	res, _ := s.GenerateContent(ctx, c.ID, llm.Stream{})
	if len(res.Messages) != 2 {
		t.Fatalf("one message per language: %+v", res.Messages)
	}
	s.Approve(ctx, c.ID)
	s.Send(ctx, c.ID)
	if v, _ := s.Get(ctx, c.ID); len(v.Variants) != 1 || v.Variants["A"]["pending"] != 42 {
		t.Errorf("variants %v", v.Variants)
	}
}

// The 40-letter name lives in the Turkish audience only, so only Turkish copy
// is held to the worst case.
func TestLimitIsCheckedPerLanguage(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, _ := s.Create(ctx, "x", []string{"sms"}, false)
	s.SetAudience(ctx, c.ID, ankaraAndBursa())
	s.GenerateContent(ctx, c.ID, llm.Stream{})
	body := "{{first_name}} " + strings.Repeat("a", 135)
	res, err := s.EditContent(ctx, c.ID, []content.Message{
		{Channel: "sms", Variant: "A", Language: "tr", Body: body},
		{Channel: "sms", Variant: "A", Language: "en", Body: body},
	})
	if err != nil || len(res.Problems) != 1 {
		t.Fatalf("only the Turkish copy is too long: %+v %v", res, err)
	}
	if _, err := s.Approve(ctx, c.ID); err == nil {
		t.Error("approval must stay blocked")
	}
	// Dropping a language the audience speaks is rejected.
	if _, err := s.EditContent(ctx, c.ID, res.Messages[:1]); err == nil {
		t.Error("missing English copy must be rejected")
	}
}
