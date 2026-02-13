package monitor

import (
	"testing"
	"time"
)

func TestStatsTracker(t *testing.T) {
	var s statsTracker

	// Initial state.
	stats := s.snapshot()
	if stats.TotalPolls != 0 {
		t.Errorf("TotalPolls = %d, want 0", stats.TotalPolls)
	}

	// Record a successful poll.
	s.recordPoll(true, 5, 100*time.Millisecond)

	stats = s.snapshot()
	if stats.TotalPolls != 1 {
		t.Errorf("TotalPolls = %d, want 1", stats.TotalPolls)
	}
	if stats.SuccessfulPolls != 1 {
		t.Errorf("SuccessfulPolls = %d, want 1", stats.SuccessfulPolls)
	}
	if stats.TotalMetrics != 5 {
		t.Errorf("TotalMetrics = %d, want 5", stats.TotalMetrics)
	}
	if stats.LastDuration != 100*time.Millisecond {
		t.Errorf("LastDuration = %v, want 100ms", stats.LastDuration)
	}

	// Record a failed poll.
	s.recordPoll(false, 0, 50*time.Millisecond)

	stats = s.snapshot()
	if stats.TotalPolls != 2 {
		t.Errorf("TotalPolls = %d, want 2", stats.TotalPolls)
	}
	if stats.FailedPolls != 1 {
		t.Errorf("FailedPolls = %d, want 1", stats.FailedPolls)
	}
	if stats.TotalMetrics != 5 {
		t.Errorf("TotalMetrics = %d, want 5 (unchanged after failure)", stats.TotalMetrics)
	}
	if stats.LastDuration != 50*time.Millisecond {
		t.Errorf("LastDuration = %v, want 50ms", stats.LastDuration)
	}
}
