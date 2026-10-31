package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/praganter/insider-case/backend/internal/llm"
)

func TestHealthz(t *testing.T) {
	ok := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("connection refused") }
	tests := []struct {
		name   string
		checks []func(context.Context) error
		want   int
	}{
		{"all up", []func(context.Context) error{ok, ok}, http.StatusOK},
		{"one down", []func(context.Context) error{ok, down}, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			registerHealth(mux, tt.checks...)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
			if rec.Code != tt.want {
				t.Fatalf("got %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

// Invalid input must be rejected before the store (nil here) is touched.
func TestPreviewRejectsBadInput(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"not json", `{`, "invalid JSON"},
		{"unknown key", `{"rule":{},"channel":"sms","sql":"1=1"}`, "invalid JSON"},
		{"unknown field name", `{"rule":{"match":"all","conditions":[{"field":"x","op":"eq","value":"y"}]},"channel":"sms"}`, `unknown field \"x\"`},
		{"bad channel", `{"rule":{"match":"all","conditions":[{"field":"city","op":"eq","value":"y"}]},"channel":"push"}`, "unknown channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newMux(nil, llm.Mock{}, nil, nil, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/audience/preview", strings.NewReader(tt.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("body %s, want it to contain %s", rec.Body, tt.want)
			}
		})
	}
}

func TestSuggestErrors(t *testing.T) {
	tests := []struct {
		name   string
		ai     llm.Client
		body   string
		status int
		want   string
	}{
		{"empty brief", llm.Mock{}, `{"brief":"  ","channels":["sms"]}`, http.StatusBadRequest, "brief"},
		{"unavailable is retryable, not mock", llm.Mock{Err: llm.ErrUnavailable}, `{"brief":"x","channels":["sms"]}`, http.StatusBadGateway, `"retry":true`},
		{"refusal", llm.Mock{Err: llm.ErrRefused}, `{"brief":"x","channels":["sms"]}`, http.StatusUnprocessableEntity, "rewrite"},
		{"no channels", llm.Mock{}, `{"brief":"x","channels":[]}`, http.StatusBadRequest, "channels"},
		{"bad channel", llm.Mock{}, `{"brief":"x","channels":["push"]}`, http.StatusBadRequest, "unknown channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newMux(nil, tt.ai, nil, nil, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/audience/suggest", strings.NewReader(tt.body)))
			if rec.Code != tt.status {
				t.Fatalf("got %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("body %s, want it to contain %s", rec.Body, tt.want)
			}
		})
	}
}

// The UI shows its MOCK badge from this, so the mode must come from the client.
func TestMode(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(nil, llm.Mock{}, nil, nil, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/mode", nil))
	if !strings.Contains(rec.Body.String(), `"mode":"mock"`) {
		t.Errorf("body %s", rec.Body)
	}
}
