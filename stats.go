package monitor

import (
	"sync"
	"time"
)

// PollStats holds polling statistics.
type PollStats struct {
	TotalPolls      int64
	SuccessfulPolls int64
	FailedPolls     int64
	TotalMetrics    int64
	LastDuration    time.Duration
}

// statsTracker provides thread-safe poll statistics tracking.
type statsTracker struct {
	mu    sync.RWMutex
	stats PollStats
}

func (s *statsTracker) recordPoll(success bool, metricCount int, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.TotalPolls++
	if success {
		s.stats.SuccessfulPolls++
		s.stats.TotalMetrics += int64(metricCount)
	} else {
		s.stats.FailedPolls++
	}
	s.stats.LastDuration = duration
}

func (s *statsTracker) snapshot() PollStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}
