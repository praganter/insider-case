package sender

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/praganter/insider-case/backend/internal/campaign"
	"github.com/praganter/insider-case/backend/internal/store"
	"github.com/praganter/insider-case/backend/internal/testdb"
	"github.com/praganter/insider-case/backend/migrations"
)

func TestProviderClassifiesResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		header     map[string]string
		body       string
		wantID     string
		retryable  bool
		retryAfter time.Duration
	}{
		{"ok", 200, nil, `{"id":"prov_1"}`, "prov_1", false, 0},
		{"ok without id is suspicious, retry", 200, nil, `{}`, "", true, 0},
		{"429 with Retry-After", 429, map[string]string{"Retry-After": "3"}, "slow", "", true, 3 * time.Second},
		{"429 without header", 429, nil, "slow", "", true, 0},
		{"500", 500, nil, "boom", "", true, 0},
		{"503", 503, nil, "boom", "", true, 0},
		{"400 is final", 400, nil, "bad recipient", "", false, 0},
		{"404 is final", 404, nil, "nope", "", false, 0},
		{"422 is final", 422, nil, "nope", "", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Idempotency-Key") != "k1" {
					t.Errorf("missing idempotency key: %q", r.Header.Get("Idempotency-Key"))
				}
				for k, v := range tt.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			id, err := NewProvider(srv.URL).Send(context.Background(), Request{Channel: "sms", To: "+1", Body: "hi", Key: "k1"})
			if tt.wantID != "" {
				if err != nil || id != tt.wantID {
					t.Fatalf("got %q, %v", id, err)
				}
				return
			}
			var se *SendError
			if !errors.As(err, &se) {
				t.Fatalf("want SendError, got %v", err)
			}
			if se.Retryable != tt.retryable || se.RetryAfter != tt.retryAfter {
				t.Errorf("retryable=%v retryAfter=%v, want %v %v", se.Retryable, se.RetryAfter, tt.retryable, tt.retryAfter)
			}
		})
	}
}

func TestProviderTimeoutIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slower than the client timeout, but bounded so Close never hangs.
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	p := NewProvider(srv.URL)
	p.HTTP.Timeout = 50 * time.Millisecond
	_, err := p.Send(context.Background(), Request{Channel: "sms", To: "+1", Key: "k"})
	var se *SendError
	if !errors.As(err, &se) || !se.Retryable {
		t.Fatalf("a timeout must be retryable, got %v", err)
	}
	// Connection refused too.
	srv.Close()
	_, err = p.Send(context.Background(), Request{Channel: "sms", To: "+1", Key: "k"})
	if !errors.As(err, &se) || !se.Retryable {
		t.Fatalf("a dead provider must be retryable, got %v", err)
	}
}

func TestBackoff(t *testing.T) {
	// Exponential with +-20% jitter, capped at 32s.
	for n, base := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 5: 16 * time.Second, 6: 32 * time.Second, 40: 32 * time.Second} {
		for range 50 {
			d := Backoff(n, errors.New("x"), nil)
			if d < time.Duration(float64(base)*0.8) || d > time.Duration(float64(base)*1.2) {
				t.Fatalf("n=%d: %v outside 80-120%% of %v", n, d, base)
			}
		}
	}
	// The provider's Retry-After wins over our own schedule.
	if d := Backoff(1, fmt.Errorf("wrapped: %w", &SendError{Retryable: true, RetryAfter: 7 * time.Second}), nil); d != 7*time.Second {
		t.Errorf("Retry-After ignored: %v", d)
	}
}

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	// DB 1 keeps these tests away from anything else using Redis.
	c := redis.NewClient(&redis.Options{Addr: addr, DB: 1})
	if err := c.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// 1000 messages from 10 concurrent workers share one 100/s budget: at least
// ~10 s in total, and no one-second window holds meaningfully more than 100.
func TestLimiterSharedBudget(t *testing.T) {
	l := &Limiter{Redis: redisClient(t), Rate: 100, Burst: 1}
	var (
		mu    sync.Mutex
		times []time.Time
		wg    sync.WaitGroup
	)
	start := time.Now()
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if err := l.Wait(context.Background(), "sms"); err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				times = append(times, time.Now())
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	if len(times) != 1000 || elapsed < 9900*time.Millisecond {
		t.Fatalf("%d messages in %v, want 1000 in at least 9.9s", len(times), elapsed)
	}
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	worst := 0
	for i := range times {
		n := 0
		for j := i; j < len(times) && times[j].Sub(times[i]) < time.Second; j++ {
			n++
		}
		worst = max(worst, n)
	}
	// 100 is the target; a few extra tolerate scheduler jitter on this side of the wire.
	if worst > 105 {
		t.Errorf("busiest one-second window held %d messages", worst)
	}
	t.Logf("1000 messages in %v, busiest second %d", elapsed.Round(10*time.Millisecond), worst)
}

func TestLimiterChannelsAreIndependent(t *testing.T) {
	l := &Limiter{Redis: redisClient(t), Rate: 10, Burst: 1}
	start := time.Now()
	for _, ch := range []string{"sms", "email"} {
		if err := l.Wait(context.Background(), ch); err != nil {
			t.Fatal(err)
		}
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Errorf("first token of each channel should be immediate, took %v", time.Since(start))
	}
}

func TestLimiterStopsOnCancel(t *testing.T) {
	l := &Limiter{Redis: redisClient(t), Rate: 1, Burst: 1}
	l.Wait(context.Background(), "sms") // drains the bucket
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx, "sms"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

// fakeProvider scripts the provider's answer per recipient and counts calls.
type fakeProvider struct {
	mu    sync.Mutex
	calls map[string]int
}

func (f *fakeProvider) handler(w http.ResponseWriter, r *http.Request) {
	var req struct{ To string }
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f.mu.Lock()
	f.calls[req.To]++
	n := f.calls[req.To]
	f.mu.Unlock()
	switch {
	case req.To == "+1": // always rejected
		http.Error(w, "invalid recipient", http.StatusBadRequest)
	case req.To == "+2" && n <= 2: // rate limited twice, then fine
		w.Header().Set("Retry-After", "0")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	case req.To == "+3": // always failing
		http.Error(w, "boom", http.StatusInternalServerError)
	default:
		fmt.Fprintf(w, `{"id":"prov_%s_%d"}`, req.To, n)
	}
}

func (f *fakeProvider) count(to string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[to]
}

// Full path with real Postgres and Redis: approved campaign -> fan-out ->
// queue -> worker -> (fake) provider -> completed.
func TestSendEndToEnd(t *testing.T) {
	ctx := context.Background()
	rdb := redisClient(t)
	dbURL := testdb.New(t)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}

	// +1 is rejected, +2 is rate limited twice, +3 always fails, +4 has a name
	// too long for the SMS template, the other 20 are fine (21 sent in total with +2).
	for i := 1; i <= 24; i++ {
		name := "Ali"
		if i == 4 {
			name = strings.Repeat("Ş", 40)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email, phone, city, language, gender, birth_year, consent_sms, consent_email, created_at)
			VALUES ($1, $2, 'Kaya', $3, $4, 'X', 'tr', 'male', 1990, true, true, now())`,
			i, name, fmt.Sprintf("u%d@example.com", i), fmt.Sprintf("+%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// A user in the city who did not consent must never be messaged.
	pool.Exec(ctx, `INSERT INTO users VALUES (99, 'No', 'Consent', 'n@example.com', '+99', 'X', 'tr', 'male', 1990, false, true, now())`)

	var campaignID int64
	err = pool.QueryRow(ctx, `INSERT INTO campaigns (brief, channels, status, segment_rule)
		VALUES ('x', '{sms}', 'approved', '{"match":"all","conditions":[{"field":"city","op":"eq","value":"X"}]}') RETURNING id`).Scan(&campaignID)
	if err != nil {
		t.Fatal(err)
	}
	// 150 characters raw: fine for "Ali", over 160 for a 40-letter name.
	body := "{{first_name}} " + strings.Repeat("a", 135)
	pool.Exec(ctx, `INSERT INTO campaign_contents (campaign_id, channel, body) VALUES ($1, 'sms', $2)`, campaignID, body)

	fake := &fakeProvider{calls: map[string]int{}}
	provider := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer provider.Close()

	redisOpt := asynq.RedisClientOpt{Addr: os.Getenv("REDIS_ADDR"), DB: 1}
	client := asynq.NewClient(redisOpt)
	defer client.Close()
	svc := &campaign.Service{Store: st, Enqueue: NewEnqueuer(client)}

	// Sending twice before any worker runs must not queue anything twice.
	n1, err := svc.Send(ctx, campaignID)
	if err != nil || n1 != 24 {
		t.Fatalf("first send queued %d, err=%v", n1, err)
	}
	n2, err := svc.Send(ctx, campaignID)
	if err != nil || n2 != 24 {
		t.Fatalf("second send saw %d pending, err=%v", n2, err)
	}
	info, err := asynq.NewInspector(redisOpt).GetQueueInfo("default")
	if err != nil || info.Pending != 24 {
		t.Fatalf("queue holds %d tasks (err=%v), want 24, no duplicates", info.Pending, err)
	}
	var rows int
	pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE campaign_id = $1`, campaignID).Scan(&rows)
	if rows != 24 {
		t.Fatalf("%d message rows, want 24 (the non-consenting user is excluded)", rows)
	}

	h := &Handler{Store: st, Provider: NewProvider(provider.URL), Limiter: &Limiter{Redis: rdb, Rate: 100000, Burst: 100}}
	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:              8,
		RetryDelayFunc:           func(int, error, *asynq.Task) time.Duration { return 20 * time.Millisecond },
		DelayedTaskCheckInterval: 50 * time.Millisecond,
		TaskCheckInterval:        50 * time.Millisecond,
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskType, h.Process)
	if err := srv.Start(mux); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	deadline := time.Now().Add(20 * time.Second)
	for {
		v, err := svc.Get(ctx, campaignID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status == campaign.Completed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("campaign still %s, messages %v", v.Status, v.Messages)
		}
		time.Sleep(50 * time.Millisecond)
	}

	v, _ := svc.Get(ctx, campaignID)
	if v.Messages["sent"] != 21 || v.Messages["failed"] != 3 || len(v.Messages) != 2 {
		t.Errorf("message counts %v, want sent=21 failed=3", v.Messages)
	}
	type row struct {
		status, lastErr string
		attempts        int
	}
	get := func(userID int) row {
		var r row
		err := pool.QueryRow(ctx, `SELECT status, attempts, coalesce(last_error, '') FROM messages WHERE campaign_id = $1 AND user_id = $2`,
			campaignID, userID).Scan(&r.status, &r.attempts, &r.lastErr)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := get(1); r.status != "failed" || r.attempts != 1 || fake.count("+1") != 1 {
		t.Errorf("rejected recipient must fail without retries: %+v, calls %d", r, fake.count("+1"))
	}
	if r := get(2); r.status != "sent" || r.attempts != 3 {
		t.Errorf("two 429s then success: %+v", r)
	}
	if r := get(3); r.status != "failed" || r.attempts != MaxAttempts || !strings.Contains(r.lastErr, "gave up") || fake.count("+3") != MaxAttempts {
		t.Errorf("persistent 5xx must stop after %d attempts: %+v, calls %d", MaxAttempts, r, fake.count("+3"))
	}
	if r := get(4); r.status != "failed" || r.attempts != 0 || fake.count("+4") != 0 || !strings.Contains(r.lastErr, "after personalization") {
		t.Errorf("over-limit for this person: %+v, calls %d", r, fake.count("+4"))
	}
	if r := get(5); r.status != "sent" || r.attempts != 1 {
		t.Errorf("plain message: %+v", r)
	}
	if fake.count("+99") != 0 {
		t.Error("a user without consent was contacted")
	}

	// Failed messages are archived (the dead letter queue), not lost.
	archived, err := asynq.NewInspector(redisOpt).ListArchivedTasks("default")
	if err != nil || len(archived) != 3 {
		t.Errorf("archived %d tasks (err=%v), want 3", len(archived), err)
	}

	// A redelivered task for a message that is already sent must not call the provider again.
	before := fake.count("+5")
	if err := h.Process(ctx, asynq.NewTask(TaskType, []byte(fmt.Sprintf(`{"message_id":%d}`, messageID(t, pool, campaignID, 5))))); err != nil {
		t.Fatal(err)
	}
	if fake.count("+5") != before {
		t.Error("duplicate delivery of a sent message")
	}
	// The campaign is done; sending it again is refused.
	var ce *campaign.ConflictError
	if _, err := svc.Send(ctx, campaignID); !errors.As(err, &ce) {
		t.Errorf("send after completion: %v", err)
	}
}

func messageID(t *testing.T, pool *pgxpool.Pool, campaignID int64, userID int) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM messages WHERE campaign_id = $1 AND user_id = $2`, campaignID, userID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
