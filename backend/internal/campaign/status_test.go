package campaign

import "testing"

func TestCanMove(t *testing.T) {
	tests := []struct {
		from, to string
		ok       bool
	}{
		{Draft, AudienceReady, true},
		{AudienceReady, ContentReady, true},
		{ContentReady, Approved, true},
		{ContentReady, AudienceReady, true},
		{Approved, Sending, true},
		{ContentReady, Scheduled, true},
		{Scheduled, Sending, true},
		{Scheduled, ContentReady, true},
		{Sending, Completed, true},
		// Nothing is sent without approval, and nothing skips a step.
		{Draft, ContentReady, false},
		{Draft, Approved, false},
		{AudienceReady, Approved, false},
		{ContentReady, Sending, false},
		{AudienceReady, Sending, false},
		// Approved campaigns cannot be edited or re-approved.
		{Approved, ContentReady, false},
		{Approved, AudienceReady, false},
		{Completed, Sending, false},
		{Approved, Scheduled, false},
		{Scheduled, Approved, false},
		{"bogus", Approved, false},
	}
	for _, tt := range tests {
		if got := CanMove(tt.from, tt.to); got != tt.ok {
			t.Errorf("%s -> %s: got %v, want %v", tt.from, tt.to, got, tt.ok)
		}
	}
}
