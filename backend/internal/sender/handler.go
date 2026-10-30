package sender

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/hibiken/asynq"

	"github.com/praganter/insider-case/backend/internal/content"
	"github.com/praganter/insider-case/backend/internal/store"
)

const (
	TaskType = "message:send"
	// MaxAttempts counts the first try, so Asynq's MaxRetry is one less.
	MaxAttempts = 5
)

// NewEnqueuer returns the function the campaign service calls per message. The
// task id makes a second enqueue of the same message a no-op.
func NewEnqueuer(c *asynq.Client) func(ctx context.Context, messageID int64) error {
	return func(ctx context.Context, messageID int64) error {
		payload, _ := json.Marshal(map[string]int64{"message_id": messageID})
		_, err := c.EnqueueContext(ctx, asynq.NewTask(TaskType, payload),
			asynq.TaskID("message-"+strconv.FormatInt(messageID, 10)),
			asynq.MaxRetry(MaxAttempts-1),
			asynq.Timeout(30*time.Second))
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil
		}
		return err
	}
}

// Backoff is the Asynq RetryDelayFunc: exponential with jitter, or the
// provider's Retry-After when it gave one. n is the number of failures so far.
func Backoff(n int, err error, _ *asynq.Task) time.Duration {
	var se *SendError
	if errors.As(err, &se) && se.RetryAfter > 0 {
		return se.RetryAfter
	}
	d := time.Second << min(max(n-1, 0), 5) // 1s, 2s, 4s ... capped at 32s
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

type Handler struct {
	Store    *store.Store
	Provider *Provider
	Limiter  *Limiter
}

func (h *Handler) Process(ctx context.Context, t *asynq.Task) error {
	var p struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad payload: %v: %w", err, asynq.SkipRetry)
	}
	m, err := h.Store.GetOutgoing(ctx, p.MessageID)
	if err != nil {
		// The message or its content vanished; nothing a retry can fix.
		return fmt.Errorf("message %d: %v: %w", p.MessageID, err, asynq.SkipRetry)
	}
	if m.Status == "sent" || m.Status == "failed" {
		return nil // a redelivered task for a finished message
	}

	// Per-person check: the approval check used the audience's longest values,
	// this one uses this person's own.
	if problems := content.Check(m.Template, m.Vars); len(problems) > 0 {
		return h.fail(ctx, m, fmt.Sprint(problems), asynq.SkipRetry)
	}
	if err := h.Store.StartAttempt(ctx, m.ID); err != nil {
		return err
	}
	if err := h.Limiter.Wait(ctx, m.Channel); err != nil {
		return err // Redis trouble or shutdown: let Asynq retry
	}

	id, err := h.Provider.Send(ctx, Request{
		Channel: m.Channel,
		To:      m.To,
		Subject: content.Render(m.Template.Subject, m.Vars),
		Body:    content.Render(m.Template.Body, m.Vars),
		Key:     fmt.Sprintf("message-%d", m.ID),
	})
	if err == nil {
		if err := h.Store.MarkSent(ctx, m.ID, id); err != nil {
			return err
		}
		return h.Store.CompleteIfDone(ctx, m.CampaignID)
	}

	var se *SendError
	if !errors.As(err, &se) || !se.Retryable {
		return h.fail(ctx, m, err.Error(), asynq.SkipRetry)
	}
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	if retried >= maxRetry {
		// Last attempt: Asynq archives the task after this error.
		return h.fail(ctx, m, fmt.Sprintf("gave up after %d attempts: %v", retried+1, err), nil)
	}
	if mErr := h.Store.MarkRetrying(ctx, m.ID, err.Error()); mErr != nil {
		return mErr
	}
	return err
}

// fail records the final failure. skip is asynq.SkipRetry to archive the task
// right away, or nil when Asynq is already out of retries.
func (h *Handler) fail(ctx context.Context, m store.Outgoing, reason string, skip error) error {
	if err := h.Store.MarkFailed(ctx, m.ID, reason); err != nil {
		return err
	}
	if err := h.Store.CompleteIfDone(ctx, m.CampaignID); err != nil {
		return err
	}
	if skip == nil {
		return errors.New(reason)
	}
	return fmt.Errorf("%s: %w", reason, skip)
}
