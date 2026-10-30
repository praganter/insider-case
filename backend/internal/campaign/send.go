package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/praganter/insider-case/backend/internal/segment"
	"github.com/praganter/insider-case/backend/internal/store"
)

// Send starts delivery of an approved campaign: it writes one message row per
// audience member and channel, then queues them. Calling it again on a campaign
// that is already sending only re-queues messages that were never queued (for
// example after a crash between the two steps); queued ones are not repeated.
func (s *Service) Send(ctx context.Context, id int64) (int, error) {
	return s.send(ctx, id, Approved)
}

// SendDue is the queued task of a scheduled campaign. It does nothing for a
// campaign that was cancelled or rescheduled to later (a stale task).
func (s *Service) SendDue(ctx context.Context, id int64) error {
	c, err := s.Store.GetCampaign(ctx, id)
	if err != nil {
		return err
	}
	if c.Status == Scheduled && (c.ScheduledAt == nil || c.ScheduledAt.After(time.Now())) {
		return nil
	}
	if c.Status != Scheduled && c.Status != Sending {
		return nil
	}
	_, err = s.send(ctx, id, Scheduled)
	return err
}

func (s *Service) send(ctx context.Context, id int64, from string) (int, error) {
	c, err := s.Store.GetCampaign(ctx, id)
	if err != nil {
		return 0, err
	}
	switch c.Status {
	case from:
		var rule segment.Node
		if err := json.Unmarshal(c.Rule, &rule); err != nil {
			return 0, err
		}
		var parts []store.FanOut
		for _, ch := range c.Channels {
			where, args, err := segment.Build(rule, ch)
			if err != nil {
				return 0, err
			}
			parts = append(parts, store.FanOut{Channel: ch, Where: where, Args: args})
		}
		if err := s.Store.StartSending(ctx, id, from, c.ABTest, parts); err != nil {
			if errors.Is(err, store.ErrStateChanged) {
				return 0, &ConflictError{c.Status, Sending}
			}
			return 0, err
		}
	case Sending:
	default:
		return 0, &ConflictError{c.Status, Sending}
	}

	ids, err := s.Store.PendingMessageIDs(ctx, id)
	if err != nil {
		return 0, err
	}
	for _, mid := range ids {
		if err := s.Enqueue(ctx, mid); err != nil {
			return 0, err
		}
	}
	// A campaign whose audience turned out empty has nothing left to wait for.
	return len(ids), s.Store.CompleteIfDone(ctx, id)
}
