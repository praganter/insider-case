package sender

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hibiken/asynq"

	"github.com/praganter/insider-case/backend/internal/store"
)

const reconcilePage = 500

// Reconcile fails the messages whose task Asynq archived without the handler
// recording a result (for example the database was down during the last
// attempt). Asynq keeps an archived task's id, so enqueueing it again would be
// a no-op; the archive itself is the reliable signal that nothing else will
// touch the message. A message still being worked on is never in the archive.
// ponytail: scans every archived task per call, fine for a PoC; page by time if the archive grows large.
func Reconcile(ctx context.Context, st *store.Store, insp *asynq.Inspector) error {
	for _, queue := range []string{"sms", "email"} {
		for page := 1; ; page++ {
			tasks, err := insp.ListArchivedTasks(queue, asynq.Page(page), asynq.PageSize(reconcilePage))
			if errors.Is(err, asynq.ErrQueueNotFound) {
				break // nothing was ever queued for this channel
			}
			if err != nil {
				return err
			}
			var ids []int64
			for _, t := range tasks {
				var p struct {
					MessageID int64 `json:"message_id"`
				}
				if json.Unmarshal(t.Payload, &p) == nil && p.MessageID > 0 {
					ids = append(ids, p.MessageID)
				}
			}
			if len(ids) > 0 {
				if err := st.FailStalled(ctx, ids, "task archived before the result was saved"); err != nil {
					return err
				}
			}
			if len(tasks) < reconcilePage {
				break
			}
		}
	}
	return nil
}
