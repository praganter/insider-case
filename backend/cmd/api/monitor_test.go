package main

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/praganter/insider-case/backend/internal/llm"
	"github.com/praganter/insider-case/backend/internal/store"
	"github.com/praganter/insider-case/backend/internal/testdb"
	"github.com/praganter/insider-case/backend/migrations"
)

func monitorServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, int64) {
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
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email, phone, city, language, gender, birth_year, consent_sms, consent_email, created_at)
		VALUES (1, 'Ali', 'Kaya', 'a@example.com', '1', 'Ankara', 'tr', 'male', 1990, true, true, now())`); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateCampaign(ctx, "brief", []string{"sms"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages (campaign_id, user_id, channel, language, status) VALUES ($1, 1, 'sms', 'tr', 'sent')`, c.ID); err != nil {
		t.Fatal(err)
	}
	// Redis DB 2: the sender tests flush DB 1 while packages run in parallel.
	var insp *asynq.Inspector
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 2})
		t.Cleanup(func() { rdb.Close() })
		if err := rdb.FlushDB(ctx).Err(); err != nil {
			t.Fatal(err)
		}
		insp = asynq.NewInspector(asynq.RedisClientOpt{Addr: addr, DB: 2})
	}
	srv := httptest.NewServer(newMux(st, llm.Mock{}, nil, nil, insp))
	t.Cleanup(srv.Close)
	return srv, pool, c.ID
}

func TestMetrics(t *testing.T) {
	srv, _, _ := monitorServer(t)
	// Two queued SMS tasks: the queue gauges read them from Redis.
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: os.Getenv("REDIS_ADDR"), DB: 2})
	defer client.Close()
	for _, id := range []string{"a", "b"} {
		if _, err := client.Enqueue(asynq.NewTask("message:send", nil), asynq.Queue("sms"), asynq.TaskID(id)); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	bufio.NewReader(resp.Body).WriteTo(&sb)
	for _, want := range []string{
		`campaign_messages{channel="sms",status="sent"} 1`,
		`campaign_message_attempts_total{channel="sms"} 0`,
		`campaigns{status="draft"} 1`,
		`campaign_queue_tasks{queue="sms",state="pending"} 2`,
		`campaign_queue_tasks{queue="sms",state="archived"} 0`,
	} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("metrics %q missing %q", sb.String(), want)
		}
	}
}

// The stream sends the current counts, then a new snapshot when a message
// changes, and ends with "done" once the campaign completes.
func TestEventsStreamUntilCompleted(t *testing.T) {
	eventsInterval = 20 * time.Millisecond
	srv, pool, id := monitorServer(t)
	if _, err := pool.Exec(context.Background(), `UPDATE campaigns SET status = 'sending' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/campaigns/" + strconv.FormatInt(id, 10) + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	lines := bufio.NewScanner(resp.Body)
	next := func(prefix string) string {
		for lines.Scan() {
			if strings.HasPrefix(lines.Text(), prefix) {
				return lines.Text()
			}
		}
		t.Fatalf("stream ended before %q", prefix)
		return ""
	}
	if got := next("data:"); !strings.Contains(got, `"sent":1`) || !strings.Contains(got, `"sending"`) {
		t.Errorf("first snapshot %s", got)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE campaigns SET status = 'completed' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if got := next("event: done"); got == "" {
		t.Error("no done event")
	}
}

func TestEventsUnknownCampaign(t *testing.T) {
	srv, _, _ := monitorServer(t)
	resp, err := http.Get(srv.URL + "/campaigns/999999/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %d, want 404", resp.StatusCode)
	}
}
