// Package campaign holds the campaign service and its status state machine.
package campaign

import "fmt"

const (
	Draft         = "draft"
	AudienceReady = "audience_ready"
	ContentReady  = "content_ready"
	Approved      = "approved"
	Scheduled     = "scheduled"
	Sending       = "sending"
	Completed     = "completed"
)

// next lists the allowed moves. Going back to audience_ready (new rule) drops
// the generated content; staying in a state means "redo this step".
var next = map[string][]string{
	Draft:         {AudienceReady},
	AudienceReady: {AudienceReady, ContentReady},
	ContentReady:  {AudienceReady, ContentReady, Approved, Scheduled},
	Approved:      {Sending},
	Scheduled:     {Sending},
	Sending:       {Completed},
}

func CanMove(from, to string) bool {
	for _, s := range next[from] {
		if s == to {
			return true
		}
	}
	return false
}

// ConflictError means the campaign is in the wrong state for the request.
type ConflictError struct{ From, To string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("campaign is %s, cannot move to %s", e.From, e.To)
}
