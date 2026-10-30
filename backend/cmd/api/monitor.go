package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/praganter/insider-case/backend/internal/campaign"
	"github.com/praganter/insider-case/backend/internal/store"
)

// How often the dashboard stream looks at the database. Polling keeps the
// workers independent of the API; a campaign has one stream per open tab.
var eventsInterval = time.Second

// registerHealth answers 503 with the failing check's error, so a probe
// shows which dependency is down.
func registerHealth(mux *http.ServeMux, checks ...func(context.Context) error) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for _, check := range checks {
			if err := check(ctx); err != nil {
				http.Error(w, "unhealthy: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.Write([]byte("ok"))
	})
}

func registerMonitor(mux *http.ServeMux, st *store.Store) {
	// Sends the message counts whenever they change and closes after the
	// campaign completes.
	mux.HandleFunc("GET /campaigns/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		sse := &sseWriter{w: w}
		tick := time.NewTicker(eventsInterval)
		defer tick.Stop()
		var last string
		for {
			c, err := st.GetCampaign(r.Context(), id)
			var counts map[string]int
			if err == nil {
				counts, err = st.MessageCounts(r.Context(), id)
			}
			if err != nil {
				if r.Context().Err() != nil {
					return
				}
				if !sse.started {
					writeErr(w, err)
					return
				}
				status, body := errorBody(err)
				body["status"] = status
				sse.send("error", body)
				return
			}
			snapshot := map[string]any{"status": c.Status, "counts": counts}
			if b, _ := json.Marshal(snapshot); string(b) != last {
				sse.send("counts", snapshot)
				last = string(b)
			}
			if c.Status == campaign.Completed {
				sse.send("done", snapshot)
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
			}
		}
	})

	// Prometheus text format by hand: one gauge family does not justify the client library.
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		stats, err := st.MessageStats(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, "# HELP campaign_messages Messages by channel and status, all campaigns.\n# TYPE campaign_messages gauge\n")
		for _, m := range stats {
			fmt.Fprintf(w, "campaign_messages{channel=%q,status=%q} %d\n", m.Channel, m.Status, m.N)
		}
	})
}
