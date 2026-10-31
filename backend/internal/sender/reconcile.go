package sender

import (
	"context"
	"errors"
	"time"

	"github.com/hibiken/asynq"

	"github.com/praganter/insider-case/backend/internal/store"
)

const (
	reconcileBatch = 500
	// reconcileIdle skips messages a worker touched recently, so one being handled right now is left alone.
	reconcileIdle = time.Minute
)

// Reconcile settles the open messages whose task Asynq archived without the
// handler recording a result (for example the database was down during the last
// attempt). Asynq keeps an archived task's id, so enqueueing it again would be a
// no-op; the archive itself is the reliable signal that nothing else will touch
// the message. The provider has the last word: a message it delivered becomes
// sent, the rest become failed. If it cannot answer, the message waits for the next run.
// ponytail: checks at most reconcileBatch of the oldest open messages per call, so a larger backlog drains over several runs.
func Reconcile(ctx context.Context, st *store.Store, insp *asynq.Inspector, prov *Provider) error {
	open, err := st.OpenMessages(ctx, reconcileIdle, reconcileBatch)
	if err != nil {
		return err
	}
	var failed []int64
	for _, m := range open {
		info, err := insp.GetTaskInfo(m.Channel, Key(m.ID))
		if errors.Is(err, asynq.ErrQueueNotFound) || errors.Is(err, asynq.ErrTaskNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if info.State != asynq.TaskStateArchived {
			continue
		}
		id, err := prov.Lookup(ctx, Key(m.ID))
		if err != nil {
			continue
		}
		if id == "" {
			failed = append(failed, m.ID)
			continue
		}
		if err := st.MarkSent(ctx, m.ID, id); err != nil {
			return err
		}
		if err := st.CompleteIfDone(ctx, m.CampaignID); err != nil {
			return err
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return st.FailStalled(ctx, failed, "task archived before the result was saved")
}
