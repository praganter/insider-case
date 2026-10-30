package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praganter/insider-case/backend/internal/segment"
	"github.com/praganter/insider-case/backend/internal/store"
	"github.com/praganter/insider-case/backend/internal/testdb"
)

var fixedNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func TestGenerateIsDeterministic(t *testing.T) {
	u1, e1 := generate(fixedNow)
	u2, e2 := generate(fixedNow)
	if !reflect.DeepEqual(u1, u2) || !reflect.DeepEqual(e1, e2) {
		t.Fatal("same seed produced different data")
	}
}

// Known counts for the fixed random seed; they change only if generate() does.
func TestGenerateKnownCounts(t *testing.T) {
	users, _ := generate(fixedNow)
	var istanbul, smsConsent, istanbulSMS int
	for _, u := range users {
		if u.City == "İstanbul" {
			istanbul++
		}
		if u.ConsentSMS {
			smsConsent++
		}
		if u.City == "İstanbul" && u.ConsentSMS {
			istanbulSMS++
		}
	}
	for name, c := range map[string][2]int{
		"users":            {len(users), 5000},
		"istanbul":         {istanbul, 1473},
		"sms consent":      {smsConsent, 3490},
		"istanbul and sms": {istanbulSMS, 1026},
	} {
		if c[0] != c[1] {
			t.Errorf("%s: got %d, want %d", name, c[0], c[1])
		}
	}
}

// Runs the real builder against a real Postgres and compares with counts
// computed in Go from the same generated data.
func TestAudienceCountsMatchSeed(t *testing.T) {
	url := testdb.New(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	now := time.Now()
	if err := run(ctx, pool, now); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}

	users, events := generate(now)
	wantBuyers := map[int64]bool{}
	for _, e := range events {
		if e.Type == "purchase" && e.At.After(now.AddDate(0, 0, -30)) {
			wantBuyers[e.UserID] = true
		}
	}
	var wantIstanbulSMS, wantNonBuyerEmail int
	for _, u := range users {
		if u.City == "İstanbul" && u.ConsentSMS {
			wantIstanbulSMS++
		}
		if u.ConsentEmail && !wantBuyers[u.ID] {
			wantNonBuyerEmail++
		}
	}

	tests := []struct {
		name, rule, channel string
		want                int
	}{
		{"city + sms consent", `{"match":"all","conditions":[{"field":"city","op":"eq","value":"İstanbul"}]}`, "sms", wantIstanbulSMS},
		{"no purchase in 30 days + email consent", `{"match":"all","conditions":[{"event":"purchase","within_days":30,"exists":false}]}`, "email", wantNonBuyerEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rule segment.Node
			if err := json.Unmarshal([]byte(tt.rule), &rule); err != nil {
				t.Fatal(err)
			}
			where, args, err := segment.Build(rule, tt.channel)
			if err != nil {
				t.Fatal(err)
			}
			got, err := st.CountAudience(ctx, where, args)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}
