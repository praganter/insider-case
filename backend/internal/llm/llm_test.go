package llm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/praganter/insider-case/backend/internal/segment"
)

func TestMockBriefs(t *testing.T) {
	tests := []struct{ name, brief, want string }{
		{
			"abandoned cart in a city",
			"İstanbul'daki, son 14 günde sepete ürün ekleyip satın almayan kullanıcılara indirim SMS'i",
			`{"match":"all","conditions":[{"field":"city","op":"eq","value":"İstanbul"},{"event":"cart_add","within_days":14,"exists":true},{"event":"purchase","within_days":14,"exists":false}]}`,
		},
		{
			"english brief, buyers",
			"Thank customers who made a purchase in the last 7 days, english speakers",
			`{"match":"all","conditions":[{"field":"language","op":"eq","value":"en"},{"event":"purchase","within_days":7,"exists":true}]}`,
		},
		{
			"unrecognized brief falls back to active users",
			"hello world",
			`{"match":"all","conditions":[{"event":"page_view","within_days":30,"exists":true}]}`,
		},
		{
			"upper case matches too",
			"İSTANBUL SEPET ENGLISH",
			`{"match":"all","conditions":[{"field":"city","op":"eq","value":"İstanbul"},{"field":"language","op":"eq","value":"en"},{"event":"cart_add","within_days":30,"exists":true}]}`,
		},
		{
			"turkish age range and gender",
			"Ankara'daki 18-24 yaş arası kadınlara öğrenci indirimi",
			`{"match":"all","conditions":[{"field":"city","op":"eq","value":"Ankara"},{"field":"gender","op":"eq","value":"female"},{"field":"age","op":"gte","value":18},{"field":"age","op":"lte","value":24}]}`,
		},
		{
			"turkish age maximum with gender suffix",
			"İzmir'deki 25 yaş altı erkeklere duyuru",
			`{"match":"all","conditions":[{"field":"city","op":"eq","value":"İzmir"},{"field":"gender","op":"eq","value":"male"},{"field":"age","op":"lt","value":25}]}`,
		},
		{
			"english age minimum, women is not men",
			"Thank English-speaking women over 40 years old who made a purchase in the last 60 days",
			`{"match":"all","conditions":[{"field":"language","op":"eq","value":"en"},{"field":"gender","op":"eq","value":"female"},{"field":"age","op":"gte","value":40},{"event":"purchase","within_days":60,"exists":true}]}`,
		},
		{
			"turkish age minimum",
			"40 yaş üstü kullanıcılar",
			`{"match":"all","conditions":[{"field":"age","op":"gte","value":40}]}`,
		},
		{
			"both genders is ambiguous, percent and days are not ages",
			"kadın ve erkek müşterilere %30 indirim, 30 gün geçerli",
			`{"match":"all","conditions":[{"event":"page_view","within_days":30,"exists":true}]}`,
		},
		{
			"absurd day count is ignored",
			"sepet 9999 gün",
			`{"match":"all","conditions":[{"event":"cart_add","within_days":30,"exists":true}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := BuildAudience(context.Background(), Mock{}, tt.brief)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(node)
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestMockOutputAlwaysValid(t *testing.T) {
	for _, brief := range []string{"", "x", "Ankara sepet satın aldı 400 gün", "İzmir english satın almayan 1 gün", "Bursa 30 yaş altı kadınlara sepet", "18 ile 65 yaş arası erkekler"} {
		node, err := BuildAudience(context.Background(), Mock{}, brief)
		if err != nil {
			t.Fatalf("%q: %v", brief, err)
		}
		if _, _, err := segment.Build(node, "email"); err != nil {
			t.Errorf("%q: %v", brief, err)
		}
	}
}

func TestRepairRound(t *testing.T) {
	node, err := BuildAudience(context.Background(), Mock{BadFirst: true}, "Ankara")
	if err != nil {
		t.Fatalf("invalid-then-valid should be repaired: %v", err)
	}
	if node.Conditions[0].Value != "Ankara" {
		t.Errorf("unexpected repaired rule: %+v", node)
	}
}

// stubClient always answers the same rule, to prove only one repair round happens.
type stubClient struct {
	rule  string
	calls int
}

func (s *stubClient) Mode() string { return "stub" }
func (s *stubClient) Audience(context.Context, string, json.RawMessage, []string) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(s.rule), nil
}

func TestInvalidAfterRepairReportsProblems(t *testing.T) {
	c := &stubClient{rule: `{"match":"all","conditions":[{"event":"purchase","within_days":999,"exists":true}]}`}
	_, err := BuildAudience(context.Background(), c, "x")
	var ire *InvalidRuleError
	if !errors.As(err, &ire) || len(ire.Problems) == 0 {
		t.Fatalf("want InvalidRuleError with problems, got %v", err)
	}
	if c.calls != 2 {
		t.Errorf("want exactly 2 calls (1 repair), got %d", c.calls)
	}
}

func TestNotJSONIsRepairable(t *testing.T) {
	c := &stubClient{rule: `not json`}
	_, err := BuildAudience(context.Background(), c, "x")
	var ire *InvalidRuleError
	if !errors.As(err, &ire) || c.calls != 2 {
		t.Fatalf("got %v after %d calls", err, c.calls)
	}
}

func TestClientErrorPassesThrough(t *testing.T) {
	_, err := BuildAudience(context.Background(), Mock{Err: ErrUnavailable}, "x")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func (s *stubClient) Content(context.Context, ContentRequest, Stream) (json.RawMessage, error) {
	s.calls++
	return json.RawMessage(s.rule), nil
}
