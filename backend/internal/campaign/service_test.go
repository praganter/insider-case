package campaign

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praganter/insider-case/backend/internal/content"
	"github.com/praganter/insider-case/backend/internal/llm"
	"github.com/praganter/insider-case/backend/internal/segment"
	"github.com/praganter/insider-case/backend/internal/store"
	"github.com/praganter/insider-case/backend/internal/testdb"
	"github.com/praganter/insider-case/backend/migrations"
)

func newService(t *testing.T, ai llm.Client) *Service {
	t.Helper()
	ctx := context.Background()
	dbURL := testdb.New(t)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// Three Ankara users (one without SMS consent, one with a very long name)
	// and one from Izmir with a short name.
	_, err = pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email, phone, city, language, gender, birth_year, consent_sms, consent_email, created_at) VALUES
		(1, 'Ali', 'Kaya', 'a@example.com', '1', 'Ankara', 'tr', 'male', 1990, true, true, now()),
		(2, $1, 'Demir', 'b@example.com', '2', 'Ankara', 'tr', 'female', 1990, true, false, now()),
		(3, 'Can', 'Arslan', 'c@example.com', '3', 'Ankara', 'tr', 'male', 1990, false, true, now()),
		(4, 'Eda', 'Celik', 'd@example.com', '4', 'Izmir', 'tr', 'female', 1990, true, true, now())`,
		strings.Repeat("Ş", 40))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: st, LLM: ai}
}

func cityRule(city string) segment.Node {
	return segment.Node{Match: "all", Conditions: []segment.Node{{Field: "city", Op: "eq", Value: city}}}
}

func status(t *testing.T, s *Service, id int64) string {
	t.Helper()
	v, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return v.Status
}

func TestHappyPath(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, err := s.Create(ctx, "Ankara'daki kullanıcılara %10 indirim", []string{"sms", "email"})
	if err != nil || c.Status != Draft {
		t.Fatalf("create: %+v %v", c, err)
	}
	counts, err := s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	if err != nil {
		t.Fatal(err)
	}
	// Consent is per channel: 2 Ankara users accept SMS, 2 accept email.
	if counts["sms"] != 2 || counts["email"] != 2 {
		t.Errorf("counts %v", counts)
	}
	if status(t, s, c.ID) != AudienceReady {
		t.Fatal("want audience_ready")
	}

	var deltas int
	res, err := s.GenerateContent(ctx, c.ID, llm.Stream{Delta: func(string) { deltas++ }})
	if err != nil || len(res.Problems) != 0 || len(res.Messages) != 2 || deltas == 0 {
		t.Fatalf("generate: %+v deltas=%d err=%v", res, deltas, err)
	}
	if status(t, s, c.ID) != ContentReady {
		t.Fatal("want content_ready")
	}
	if _, err := s.Approve(ctx, c.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	v, _ := s.Get(ctx, c.ID)
	if v.Status != Approved || v.ApprovedAt == nil || len(v.Contents) != 2 {
		t.Errorf("after approve: %+v", v)
	}
}

func TestCannotSkipSteps(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	var ce *ConflictError
	if _, err := s.Approve(ctx, c.ID); !errors.As(err, &ce) {
		t.Errorf("approve from draft: %v", err)
	}
	if _, err := s.GenerateContent(ctx, c.ID, llm.Stream{}); !errors.As(err, &ce) {
		t.Errorf("generate from draft: %v", err)
	}
	if _, err := s.EditContent(ctx, c.ID, nil); !errors.As(err, &ce) {
		t.Errorf("edit from draft: %v", err)
	}
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	if _, err := s.Approve(ctx, c.ID); !errors.As(err, &ce) {
		t.Errorf("approve without content: %v", err)
	}
	if status(t, s, c.ID) != AudienceReady {
		t.Error("failed requests must not change the status")
	}
}

// The 161st character blocks approval; fixing the copy unblocks it.
func TestOverLimitBlocksApprovalUntilFixed(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	if _, err := s.GenerateContent(ctx, c.ID, llm.Stream{}); err != nil {
		t.Fatal(err)
	}

	res, err := s.EditContent(ctx, c.ID, []content.Message{{Channel: "sms", Body: strings.Repeat("a", 161)}})
	if err != nil || len(res.Problems) == 0 {
		t.Fatalf("161 chars should be reported: %+v %v", res, err)
	}
	var le *LimitError
	if _, err := s.Approve(ctx, c.ID); !errors.As(err, &le) || !strings.Contains(le.Error(), "161") {
		t.Fatalf("approve should be blocked: %v", err)
	}
	if status(t, s, c.ID) != ContentReady {
		t.Error("blocked approval must leave the status alone")
	}

	res, err = s.EditContent(ctx, c.ID, []content.Message{{Channel: "sms", Body: strings.Repeat("a", 160)}})
	if err != nil || len(res.Problems) != 0 {
		t.Fatalf("160 chars should pass: %+v %v", res, err)
	}
	if _, err := s.Approve(ctx, c.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}
}

// 150 characters fit with short names, but user 2 has a 40-letter first name.
func TestLongestNameInAudienceBlocksApproval(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	s.GenerateContent(ctx, c.ID, llm.Stream{})

	body := "{{first_name}} " + strings.Repeat("a", 135) // 150 raw, 175 with the 40-letter name
	res, _ := s.EditContent(ctx, c.ID, []content.Message{{Channel: "sms", Body: body}})
	if len(res.Problems) == 0 || !strings.Contains(res.Problems[0], "after personalization") {
		t.Fatalf("want worst-case violation, got %v", res.Problems)
	}
	var le *LimitError
	if _, err := s.Approve(ctx, c.ID); !errors.As(err, &le) {
		t.Fatalf("approve should be blocked: %v", err)
	}

	// Narrowing the audience to the short-named user makes the same copy fine.
	if _, err := s.SetAudience(ctx, c.ID, cityRule("Izmir")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateContent(ctx, c.ID, llm.Stream{}); err != nil {
		t.Fatal(err)
	}
	res, err := s.EditContent(ctx, c.ID, []content.Message{{Channel: "sms", Body: body}})
	if err != nil || len(res.Problems) != 0 {
		t.Errorf("short names only: %+v %v", res, err)
	}
	if _, err := s.Approve(ctx, c.ID); err != nil {
		t.Errorf("approve: %v", err)
	}
}

func TestEmptyAudienceBlocksApproval(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	counts, err := s.SetAudience(ctx, c.ID, cityRule("Nowhere"))
	if err != nil || counts["sms"] != 0 {
		t.Fatalf("zero audience is allowed: %v %v", counts, err)
	}
	s.GenerateContent(ctx, c.ID, llm.Stream{})
	var le *LimitError
	if _, err := s.Approve(ctx, c.ID); !errors.As(err, &le) || !strings.Contains(le.Error(), "audience is empty") {
		t.Fatalf("got %v", err)
	}
}

func TestChangingAudienceDropsContent(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	s.GenerateContent(ctx, c.ID, llm.Stream{})
	if _, err := s.SetAudience(ctx, c.ID, cityRule("Ankara")); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(ctx, c.ID)
	if v.Status != AudienceReady || len(v.Contents) != 0 {
		t.Errorf("content must go with the old rule: %+v", v)
	}
}

func TestApprovedCampaignIsFrozen(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	s.GenerateContent(ctx, c.ID, llm.Stream{})
	if _, err := s.Approve(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := s.SetAudience(ctx, c.ID, cityRule("Ankara")); !errors.As(err, &ce) {
		t.Errorf("audience: %v", err)
	}
	if _, err := s.GenerateContent(ctx, c.ID, llm.Stream{}); !errors.As(err, &ce) {
		t.Errorf("generate: %v", err)
	}
	if _, err := s.EditContent(ctx, c.ID, []content.Message{{Channel: "sms", Body: "new"}}); !errors.As(err, &ce) {
		t.Errorf("edit: %v", err)
	}
	if _, err := s.Approve(ctx, c.ID); !errors.As(err, &ce) {
		t.Errorf("approve twice: %v", err)
	}
}

func TestInvalidInput(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	var ie *InputError
	for _, ch := range [][]string{nil, {"push"}, {"sms", "sms"}} {
		if _, err := s.Create(ctx, "x", ch); !errors.As(err, &ie) {
			t.Errorf("channels %v: %v", ch, err)
		}
	}
	if _, err := s.Create(ctx, " ", []string{"sms"}); !errors.As(err, &ie) {
		t.Errorf("empty brief: %v", err)
	}
	c, _ := s.Create(ctx, "x", []string{"sms"})
	bad := segment.Node{Match: "all", Conditions: []segment.Node{{Field: "password", Op: "eq", Value: "x"}}}
	if _, err := s.SetAudience(ctx, c.ID, bad); !errors.As(err, &ie) {
		t.Errorf("bad rule: %v", err)
	}
	if _, err := s.Get(ctx, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing campaign: %v", err)
	}
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	s.GenerateContent(ctx, c.ID, llm.Stream{})
	if _, err := s.EditContent(ctx, c.ID, []content.Message{{Channel: "email", Subject: "s", Body: "b"}}); !errors.As(err, &ie) {
		t.Errorf("wrong channel set: %v", err)
	}
}

func TestLLMFailureLeavesCampaignInPlace(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{Err: llm.ErrUnavailable})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	if _, err := s.GenerateContent(ctx, c.ID, llm.Stream{}); !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if status(t, s, c.ID) != AudienceReady {
		t.Error("campaign must stay audience_ready so the user can retry")
	}
}

func TestRepairedContentIsStored(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{BadFirst: true})
	c, _ := s.Create(ctx, "x", []string{"sms"})
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	res, err := s.GenerateContent(ctx, c.ID, llm.Stream{})
	if err != nil || len(res.Problems) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	v, _ := s.Get(ctx, c.ID)
	if len(v.Contents) != 1 || len(v.Contents[0].Body) > 160 {
		t.Errorf("stored content: %+v", v.Contents)
	}
}
