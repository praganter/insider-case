package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func post(h http.Handler, path, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const smsBody = `{"to":"+905550000001","body":"hi"}`

func TestDeliversAndIdempotent(t *testing.T) {
	p := newProvider(config{LimitPerSec: 100})
	h := p.handler()
	first := post(h, "/sms", "k1", smsBody)
	again := post(h, "/sms", "k1", smsBody)
	other := post(h, "/sms", "k2", smsBody)
	if first.Code != 200 || first.Body.String() != again.Body.String() {
		t.Fatalf("same key must return the same answer: %q vs %q", first.Body, again.Body)
	}
	if other.Body.String() == first.Body.String() {
		t.Error("a different key is a different message")
	}
	if p.next != 2 {
		t.Errorf("delivered %d times, want 2 (the repeat is not delivered again)", p.next)
	}
}

func TestLookupFindsOnlyDeliveredKeys(t *testing.T) {
	// Every send fails, so a key can only be known if it was delivered before.
	p := newProvider(config{LimitPerSec: 100})
	h := p.handler()
	get := func(key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/messages/"+key, nil))
		return rec
	}
	if got := get("k1"); got.Code != 404 {
		t.Fatalf("unknown key: %d, want 404", got.Code)
	}
	sent := post(h, "/sms", "k1", smsBody)
	if got := get("k1"); got.Code != 200 || got.Body.String() != sent.Body.String() {
		t.Errorf("delivered key: %d %q, want the id of the send %q", got.Code, got.Body, sent.Body)
	}
	p.cfg.ErrorRate = 1
	post(h, "/sms", "k2", smsBody)
	if got := get("k2"); got.Code != 404 {
		t.Errorf("failed send must not look delivered: %d", got.Code)
	}
}

func TestRejectsInvalidRecipients(t *testing.T) {
	h := newProvider(config{LimitPerSec: 100}).handler()
	if rec := post(h, "/sms", "a", `{"to":"05550000001","body":"x"}`); rec.Code != 400 {
		t.Errorf("sms without + prefix: %d", rec.Code)
	}
	if rec := post(h, "/email", "b", `{"to":"nobody","subject":"s","body":"x"}`); rec.Code != 400 {
		t.Errorf("email without @: %d", rec.Code)
	}
	if rec := post(h, "/email", "c", `{"to":"a@example.com","subject":"s","body":"x"}`); rec.Code != 200 {
		t.Errorf("valid email: %d", rec.Code)
	}
}

func TestInjectedErrors(t *testing.T) {
	always := func() float64 { return 0 }
	h := newProvider(config{LimitPerSec: 100, ErrorRate: 1, randFloat: always}).handler()
	if rec := post(h, "/sms", "a", smsBody); rec.Code != 500 {
		t.Errorf("error rate 1: %d", rec.Code)
	}
	h = newProvider(config{LimitPerSec: 100, TooManyRate: 1, randFloat: always}).handler()
	rec := post(h, "/sms", "a", smsBody)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "1" {
		t.Errorf("429 rate 1: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// The provider enforces its own limit per channel and second; above it, 429.
func TestEnforcesPerChannelRateLimit(t *testing.T) {
	now := time.Unix(1000, 0)
	p := newProvider(config{LimitPerSec: 100, clock: func() time.Time { return now }, randFloat: func() float64 { return 1 }})
	h := p.handler()
	var ok, limited int
	for i := range 130 {
		switch post(h, "/sms", "key"+string(rune('A'+i%26))+strings.Repeat("x", i), smsBody).Code {
		case 200:
			ok++
		case 429:
			limited++
		}
	}
	if ok != 100 || limited != 30 {
		t.Fatalf("ok=%d limited=%d, want 100 and 30", ok, limited)
	}
	// Email has its own budget in the same second.
	if rec := post(h, "/email", "e1", `{"to":"a@example.com","subject":"s","body":"b"}`); rec.Code != 200 {
		t.Errorf("email budget is separate: %d", rec.Code)
	}
	// A new second brings a fresh budget.
	now = now.Add(time.Second)
	if rec := post(h, "/sms", "fresh", smsBody); rec.Code != 200 {
		t.Errorf("next second: %d", rec.Code)
	}
}
