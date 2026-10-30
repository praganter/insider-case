package sender

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/praganter/insider-case/backend/internal/campaign"
)

// CampaignTaskType is the queued "start this scheduled campaign" task.
const CampaignTaskType = "campaign:send"

// NewScheduler returns the function the campaign service calls when a campaign
// is scheduled; Asynq holds the task until at.
func NewScheduler(c *asynq.Client) func(ctx context.Context, campaignID int64, at time.Time) error {
	return func(ctx context.Context, campaignID int64, at time.Time) error {
		payload, _ := json.Marshal(map[string]int64{"campaign_id": campaignID})
		_, err := c.EnqueueContext(ctx, asynq.NewTask(CampaignTaskType, payload), asynq.ProcessAt(at))
		return err
	}
}

// CampaignHandler runs the scheduled task: it starts the campaign's delivery.
func CampaignHandler(svc *campaign.Service) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var p struct {
			CampaignID int64 `json:"campaign_id"`
		}
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("bad payload: %v: %w", err, asynq.SkipRetry)
		}
		return svc.SendDue(ctx, p.CampaignID)
	}
}
