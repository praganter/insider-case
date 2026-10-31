package campaign

import (
	"context"
	"testing"

	"github.com/praganter/insider-case/backend/internal/llm"
)

func TestListCampaignsNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := newService(t, llm.Mock{})

	list, err := s.Store.ListCampaigns(ctx, 50)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty list should be [], got %v, %v", list, err)
	}

	first, _ := s.Create(ctx, "first", []string{"sms"}, false)
	second, _ := s.Create(ctx, "second", []string{"sms"}, false)
	list, err = s.Store.ListCampaigns(ctx, 1)
	if err != nil || len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("limit 1 should give the newest (%d), got %+v, %v", second.ID, list, err)
	}
	list, _ = s.Store.ListCampaigns(ctx, 50)
	if len(list) != 2 || list[1].ID != first.ID || list[0].Status != Draft {
		t.Errorf("unexpected list: %+v", list)
	}
}
