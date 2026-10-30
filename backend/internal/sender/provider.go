// Package sender delivers messages: the queue task handler, the provider
// client with retry classification, and the shared Redis rate limit.
package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// SendError says whether trying again can help.
type SendError struct {
	Status     int // 0 for network errors and timeouts
	Retryable  bool
	RetryAfter time.Duration // from the provider's Retry-After header
	Msg        string
}

func (e *SendError) Error() string { return e.Msg }

type Request struct {
	Channel string
	To      string
	Subject string
	Body    string
	// Key makes a repeated delivery of the same message a no-op at the provider.
	Key string
}

type Provider struct {
	BaseURL string
	HTTP    *http.Client
}

func NewProvider(baseURL string) *Provider {
	return &Provider{BaseURL: baseURL, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Send returns the provider's message id. Timeouts, 429 and 5xx are retryable;
// every other 4xx is not (the message itself is wrong, retrying won't fix it).
func (p *Provider) Send(ctx context.Context, r Request) (string, error) {
	payload, _ := json.Marshal(map[string]string{"to": r.To, "subject": r.Subject, "body": r.Body})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/"+r.Channel, bytes.NewReader(payload))
	if err != nil {
		return "", &SendError{Msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", r.Key)
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return "", &SendError{Retryable: true, Msg: "provider unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	switch {
	case resp.StatusCode/100 == 2:
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &out); err != nil || out.ID == "" {
			return "", &SendError{Status: resp.StatusCode, Retryable: true, Msg: "provider answered 2xx without an id"}
		}
		return out.ID, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return "", &SendError{Status: 429, Retryable: true, RetryAfter: time.Duration(secs) * time.Second, Msg: "provider rate limited (429)"}
	case resp.StatusCode >= 500:
		return "", &SendError{Status: resp.StatusCode, Retryable: true, Msg: fmt.Sprintf("provider error %d", resp.StatusCode)}
	}
	return "", &SendError{Status: resp.StatusCode, Msg: fmt.Sprintf("provider rejected the message (%d): %s", resp.StatusCode, body)}
}
