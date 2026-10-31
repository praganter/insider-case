package campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/praganter/insider-case/backend/internal/llm"
)

// contentReady returns a service and a campaign that can be approved.
func contentReady(t *testing.T) (*Service, int64, *[]time.Time) {
	t.Helper()
	ctx := context.Background()
	s := newService(t, llm.Mock{})
	var queuedAt []time.Time
	s.EnqueueAt = func(_ context.Context, _ int64, at time.Time) error { queuedAt = append(queuedAt, at); return nil }
	s.Enqueue = func(context.Context, int64, string) error { return nil }
	c, _ := s.Create(ctx, "Ankara'daki kullanıcılara %10 indirim", []string{"sms"}, false)
	s.SetAudience(ctx, c.ID, cityRule("Ankara"))
	if _, err := s.GenerateContent(ctx, c.ID, llm.Stream{}); err != nil {
		t.Fatal(err)
	}
	return s, c.ID, &queuedAt
}

func TestScheduleThenSendDue(t *testing.T) {
	ctx := context.Background()
	s, id, queuedAt := contentReady(t)

	at := time.Now().Add(300 * time.Millisecond)
	if _, err := s.Schedule(ctx, id, at); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(ctx, id)
	if v.Status != Scheduled || v.ScheduledAt == nil || len(*queuedAt) != 1 || !(*queuedAt)[0].Equal(at) {
		t.Fatalf("after schedule: %+v queued %v", v.Campaign, *queuedAt)
	}

	// A manual send must not jump the schedule, and an early task does nothing.
	var ce *ConflictError
	if _, err := s.Send(ctx, id); !errors.As(err, &ce) {
		t.Errorf("manual send of a scheduled campaign: %v", err)
	}
	if err := s.SendDue(ctx, id); err != nil || status(t, s, id) != Scheduled {
		t.Errorf("early SendDue: err=%v status=%s", err, status(t, s, id))
	}

	// Once the time has come the task starts the send; a repeated task is harmless.
	time.Sleep(400 * time.Millisecond)
	for range 2 {
		if err := s.SendDue(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	v, _ = s.Get(ctx, id)
	if v.Status != Sending || v.Messages["pending"] != 2 {
		t.Errorf("after SendDue: %s %v", v.Status, v.Messages)
	}
}

func TestUnscheduleMakesTaskStale(t *testing.T) {
	ctx := context.Background()
	s, id, _ := contentReady(t)
	s.Schedule(ctx, id, time.Now().Add(300*time.Millisecond))
	if err := s.Unschedule(ctx, id); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(ctx, id)
	if v.Status != ContentReady || v.ScheduledAt != nil || v.ApprovedAt != nil {
		t.Errorf("after unschedule: %+v", v.Campaign)
	}
	time.Sleep(400 * time.Millisecond)
	if err := s.SendDue(ctx, id); err != nil || status(t, s, id) != ContentReady {
		t.Errorf("stale task must do nothing: err=%v status=%s", err, status(t, s, id))
	}
	var ce *ConflictError
	if err := s.Unschedule(ctx, id); !errors.As(err, &ce) {
		t.Errorf("unschedule twice: %v", err)
	}
}

func TestScheduleRejections(t *testing.T) {
	ctx := context.Background()
	s, id, queuedAt := contentReady(t)
	var ie *InputError
	if _, err := s.Schedule(ctx, id, time.Now().Add(-time.Minute)); !errors.As(err, &ie) {
		t.Errorf("past time: %v", err)
	}
	// Scheduling shares Approve's checks: a campaign without content cannot be scheduled.
	d, _ := s.Create(ctx, "x", []string{"sms"}, false)
	var ce *ConflictError
	if _, err := s.Schedule(ctx, d.ID, time.Now().Add(time.Hour)); !errors.As(err, &ce) {
		t.Errorf("schedule from draft: %v", err)
	}
	if len(*queuedAt) != 0 {
		t.Errorf("a rejected schedule must queue nothing: %v", *queuedAt)
	}
}
