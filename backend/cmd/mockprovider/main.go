// Command mockprovider imitates an SMS/email provider: configurable errors,
// delays and 429s, a hard per-channel rate limit, and idempotency keys.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type config struct {
	ErrorRate   float64       // share of requests answered with a 500
	TooManyRate float64       // share of requests answered with a random 429
	Delay       time.Duration // added to every request
	LimitPerSec int           // real per-channel limit; above it the answer is 429
	clock       func() time.Time
	randFloat   func() float64
}

type provider struct {
	cfg config

	mu     sync.Mutex
	seen   map[string]string // idempotency key -> provider id
	window map[string]int    // channel -> accepted this second
	second int64
	next   int
}

func newProvider(cfg config) *provider {
	if cfg.clock == nil {
		cfg.clock = time.Now
	}
	if cfg.randFloat == nil {
		cfg.randFloat = rand.Float64
	}
	return &provider{cfg: cfg, seen: map[string]string{}, window: map[string]int{}}
}

func (p *provider) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sms", p.send("sms"))
	mux.HandleFunc("POST /email", p.send("email"))
	return mux
}

func (p *provider) send(channel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(p.cfg.Delay)
		var req struct {
			To      string `json:"to"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if (channel == "sms" && !strings.HasPrefix(req.To, "+")) || (channel == "email" && !strings.Contains(req.To, "@")) {
			http.Error(w, "invalid recipient", http.StatusBadRequest)
			return
		}
		key := r.Header.Get("Idempotency-Key")

		p.mu.Lock()
		defer p.mu.Unlock()
		// A repeated key is answered with the original result and not delivered again.
		if id, ok := p.seen[key]; ok && key != "" {
			writeID(w, id)
			return
		}
		if p.cfg.randFloat() < p.cfg.ErrorRate {
			http.Error(w, "provider error", http.StatusInternalServerError)
			return
		}
		if p.cfg.randFloat() < p.cfg.TooManyRate {
			tooMany(w)
			return
		}
		sec := p.cfg.clock().Unix()
		if sec != p.second {
			// One line per channel and second, so "never above the limit" can be read from the logs.
			for ch, n := range p.window {
				log.Printf("rate %s=%d/s", ch, n)
			}
			p.second = sec
			clear(p.window)
		}
		if p.window[channel] >= p.cfg.LimitPerSec {
			log.Printf("rate limit exceeded on %s (%d/s)", channel, p.cfg.LimitPerSec)
			tooMany(w)
			return
		}
		p.window[channel]++
		p.next++
		id := fmt.Sprintf("prov_%d", p.next)
		if key != "" {
			p.seen[key] = id
		}
		writeID(w, id)
	}
}

func tooMany(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "slow down", http.StatusTooManyRequests)
}

func writeID(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func envFloat(name string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(name), 64); err == nil {
		return v
	}
	return def
}

func main() {
	p := newProvider(config{
		ErrorRate:   envFloat("ERROR_RATE", 0.05),
		TooManyRate: envFloat("TOO_MANY_RATE", 0.05),
		Delay:       time.Duration(envFloat("DELAY_MS", 20)) * time.Millisecond,
		LimitPerSec: int(envFloat("LIMIT_PER_SEC", 100)),
	})
	addr := ":" + os.Getenv("PROVIDER_PORT")
	if addr == ":" {
		addr = ":9090"
	}
	log.Printf("mock provider on %s (errors %.0f%%, 429s %.0f%%, limit %d/s per channel)",
		addr, p.cfg.ErrorRate*100, p.cfg.TooManyRate*100, p.cfg.LimitPerSec)
	log.Fatal(http.ListenAndServe(addr, p.handler()))
}
