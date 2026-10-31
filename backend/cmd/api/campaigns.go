package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/praganter/insider-case/backend/internal/campaign"
	"github.com/praganter/insider-case/backend/internal/content"
	"github.com/praganter/insider-case/backend/internal/llm"
	"github.com/praganter/insider-case/backend/internal/segment"
	"github.com/praganter/insider-case/backend/internal/store"
)

func registerCampaigns(mux *http.ServeMux, svc *campaign.Service) {
	mux.HandleFunc("POST /campaigns", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Brief    string   `json:"brief"`
			Channels []string `json:"channels"`
			ABTest   bool     `json:"ab_test"`
		}
		if !decode(w, r, &req) {
			return
		}
		c, err := svc.Create(r.Context(), req.Brief, req.Channels, req.ABTest)
		reply(w, err, http.StatusCreated, c)
	})

	mux.HandleFunc("GET /campaigns", func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.Store.ListCampaigns(r.Context(), 50)
		reply(w, err, http.StatusOK, list)
	})

	mux.HandleFunc("GET /campaigns/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		v, err := svc.Get(r.Context(), id)
		reply(w, err, http.StatusOK, v)
	})

	mux.HandleFunc("PUT /campaigns/{id}/audience", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var req struct {
			Rule segment.Node `json:"rule"`
		}
		if !decode(w, r, &req) {
			return
		}
		counts, err := svc.SetAudience(r.Context(), id, req.Rule)
		reply(w, err, http.StatusOK, map[string]any{"counts": counts})
	})

	// POST rather than GET: it spends LLM tokens and changes the campaign.
	mux.HandleFunc("POST /campaigns/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		sse := &sseWriter{w: w}
		res, err := svc.GenerateContent(r.Context(), id, llm.Stream{
			Delta: func(d string) { sse.send("delta", map[string]string{"text": d}) },
			Reset: func() { sse.send("reset", struct{}{}) },
		})
		switch {
		case err == nil:
			sse.send("done", res)
		case sse.started:
			status, body := errorBody(err)
			body["status"] = status
			sse.send("error", body)
		default:
			writeErr(w, err)
		}
	})

	mux.HandleFunc("PUT /campaigns/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var req struct {
			Messages []content.Message `json:"messages"`
		}
		if !decode(w, r, &req) {
			return
		}
		res, err := svc.EditContent(r.Context(), id, req.Messages)
		reply(w, err, http.StatusOK, res)
	})

	mux.HandleFunc("POST /campaigns/{id}/send", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		n, err := svc.Send(r.Context(), id)
		reply(w, err, http.StatusAccepted, map[string]any{"queued": n})
	})

	mux.HandleFunc("POST /campaigns/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		at, err := svc.Approve(r.Context(), id)
		reply(w, err, http.StatusOK, map[string]any{"status": campaign.Approved, "approved_at": at})
	})

	// Approval with a send time; the queue starts the campaign then.
	mux.HandleFunc("POST /campaigns/{id}/schedule", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var req struct {
			At time.Time `json:"at"`
		}
		if !decode(w, r, &req) {
			return
		}
		approvedAt, err := svc.Schedule(r.Context(), id, req.At)
		reply(w, err, http.StatusOK, map[string]any{"status": campaign.Scheduled, "approved_at": approvedAt, "scheduled_at": req.At})
	})

	mux.HandleFunc("POST /campaigns/{id}/unschedule", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		reply(w, svc.Unschedule(r.Context(), id), http.StatusOK, map[string]any{"status": campaign.ContentReady})
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []string{"invalid JSON: " + err.Error()}})
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"errors": []string{"campaign not found"}})
		return 0, false
	}
	return id, true
}

func reply(w http.ResponseWriter, err error, status int, body any) {
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, status, body)
}

func writeErr(w http.ResponseWriter, err error) {
	status, body := errorBody(err)
	writeJSON(w, status, body)
}

// errorBody maps service and LLM errors to an HTTP status and a JSON body.
func errorBody(err error) (int, map[string]any) {
	var (
		ce  *campaign.ConflictError
		ie  *campaign.InputError
		le  *campaign.LimitError
		ice *llm.InvalidContentError
	)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, map[string]any{"errors": []string{"campaign not found"}}
	case errors.As(err, &ce):
		return http.StatusConflict, map[string]any{"errors": []string{ce.Error()}}
	case errors.As(err, &ie):
		return http.StatusBadRequest, map[string]any{"errors": ie.Problems}
	case errors.As(err, &le):
		return http.StatusUnprocessableEntity, map[string]any{"errors": le.Problems}
	case errors.As(err, &ice):
		return http.StatusUnprocessableEntity, map[string]any{"errors": ice.Problems, "raw": ice.Raw}
	case errors.Is(err, llm.ErrRefused):
		return http.StatusUnprocessableEntity, map[string]any{"errors": []string{"The brief could not be processed, please rewrite it."}}
	case errors.Is(err, llm.ErrUnavailable), errors.Is(err, llm.ErrTruncated):
		log.Printf("llm: %v", err)
		return http.StatusBadGateway, map[string]any{"errors": []string{"The LLM is unavailable right now, please try again."}, "retry": true}
	}
	log.Printf("campaigns: %v", err)
	return http.StatusInternalServerError, map[string]any{"errors": []string{"internal error"}}
}

// sseWriter sends Server-Sent Events. The headers go out with the first event,
// so errors before that can still use a normal HTTP status.
type sseWriter struct {
	w       http.ResponseWriter
	started bool
}

func (s *sseWriter) send(event string, data any) {
	if !s.started {
		s.w.Header().Set("Content-Type", "text/event-stream")
		s.w.Header().Set("Cache-Control", "no-cache")
		s.started = true
	}
	b, _ := json.Marshal(data)
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b)
	http.NewResponseController(s.w).Flush()
}
