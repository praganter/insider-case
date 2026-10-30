package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/praganter/insider-case/backend/internal/campaign"
	"github.com/praganter/insider-case/backend/internal/llm"
	"github.com/praganter/insider-case/backend/internal/segment"
	"github.com/praganter/insider-case/backend/internal/sender"
	"github.com/praganter/insider-case/backend/internal/store"
)

func newMux(st *store.Store, ai llm.Client, enqueue func(context.Context, int64) error) *http.ServeMux {
	mux := http.NewServeMux()
	registerCampaigns(mux, &campaign.Service{Store: st, LLM: ai, Enqueue: enqueue})
	registerMonitor(mux, st)
	mux.HandleFunc("POST /audience/preview", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Rule    segment.Node `json:"rule"`
			Channel string       `json:"channel"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []string{"invalid JSON: " + err.Error()}})
			return
		}
		where, args, err := segment.Build(req.Rule, req.Channel)
		var ve *segment.ValidationError
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errors": ve.Problems})
			return
		}
		count, err := st.CountAudience(r.Context(), where, args)
		if err != nil {
			log.Printf("audience preview: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"errors": []string{"could not count audience"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": count})
	})
	mux.HandleFunc("POST /audience/suggest", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Brief   string `json:"brief"`
			Channel string `json:"channel"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.Brief) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []string{`body must be {"brief": "...", "channel": "sms|email"}`}})
			return
		}
		rule, err := llm.BuildAudience(r.Context(), ai, req.Brief)
		var ire *llm.InvalidRuleError
		switch {
		case errors.As(err, &ire):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": ire.Problems, "rule": ire.Rule, "mode": ai.Mode()})
			return
		case errors.Is(err, llm.ErrRefused):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": []string{"The brief could not be processed, please rewrite it."}, "mode": ai.Mode()})
			return
		case err != nil:
			log.Printf("audience suggest: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"errors": []string{"The LLM is unavailable right now, please try again."}, "retry": true, "mode": ai.Mode()})
			return
		}
		where, args, err := segment.Build(rule, req.Channel)
		var ve *segment.ValidationError
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errors": ve.Problems})
			return
		}
		count, err := st.CountAudience(r.Context(), where, args)
		if err != nil {
			log.Printf("audience suggest: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"errors": []string{"could not count audience"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rule": rule, "count": count, "mode": ai.Mode()})
	})
	return mux
}

// newLLM picks the mode once at startup; with a key set it never falls back to the mock.
func newLLM() llm.Client {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		log.Print("ANTHROPIC_API_KEY is empty: running in MOCK LLM mode")
		return llm.Mock{Delay: 25 * time.Millisecond}
	}
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-opus-5-5"
	}
	return llm.NewAnthropic(key, model)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	st, err := store.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: os.Getenv("REDIS_ADDR")})
	defer queue.Close()
	enqueue := sender.NewEnqueuer(queue)
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})
	defer rdb.Close()
	addr := ":" + os.Getenv("API_PORT")
	if addr == ":" {
		addr = ":8080"
	}
	log.Printf("api listening on %s", addr)
	mux := newMux(st, newLLM(), enqueue)
	registerHealth(mux, st.Ping, func(ctx context.Context) error { return rdb.Ping(ctx).Err() })
	log.Fatal(http.ListenAndServe(addr, mux))
}
