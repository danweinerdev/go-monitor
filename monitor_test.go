package monitor

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMonitorRunOnce(t *testing.T) {
	collected := false
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		collected = true
		return []*Metric{
			NewMetric("test").WithField("value", 42),
		}, nil
	}

	var buf bytes.Buffer
	echo := NewEcho(&buf, nil)

	m, err := New("test-monitor", collectFn,
		WithRunOnce(true),
		WithBackend(echo),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if !collected {
		t.Error("CollectFunc should have been called")
	}

	output := buf.String()
	if !strings.Contains(output, "test") {
		t.Errorf("Output should contain metric, got %q", output)
	}
}

func TestMonitorRunOnceWithConfig(t *testing.T) {
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		return []*Metric{
			NewMetric("test").WithField("value", 1),
		}, nil
	}

	cfg := DefaultConfig()
	cfg.Global.PollInterval = Duration{5 * time.Second}

	var buf bytes.Buffer

	m, err := New("test", collectFn,
		WithConfig(cfg),
		WithRunOnce(true),
		WithBackend(NewEcho(&buf, nil)),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestMonitorStats(t *testing.T) {
	callCount := 0
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		callCount++
		return []*Metric{
			NewMetric("test").WithField("value", callCount),
		}, nil
	}

	m, err := New("test", collectFn,
		WithRunOnce(true),
		WithBackend(&mockBackend{name: "test", healthy: true}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	m.Run(context.Background())

	stats := m.Stats()
	if stats.TotalPolls != 1 {
		t.Errorf("TotalPolls = %d, want 1", stats.TotalPolls)
	}
	if stats.SuccessfulPolls != 1 {
		t.Errorf("SuccessfulPolls = %d, want 1", stats.SuccessfulPolls)
	}
	if stats.TotalMetrics != 1 {
		t.Errorf("TotalMetrics = %d, want 1", stats.TotalMetrics)
	}
}

func TestMonitorCollectionError(t *testing.T) {
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		return nil, fmt.Errorf("collection failed")
	}

	m, err := New("test", collectFn,
		WithRunOnce(true),
		WithBackend(&mockBackend{name: "test", healthy: true}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	m.Run(context.Background())

	stats := m.Stats()
	if stats.FailedPolls != 1 {
		t.Errorf("FailedPolls = %d, want 1", stats.FailedPolls)
	}
}

func TestMonitorNoBackends(t *testing.T) {
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		return nil, nil
	}

	m, err := New("test", collectFn)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	err = m.Run(context.Background())
	if err == nil {
		t.Error("Run() should error with no backends")
	}
}

func TestMonitorWithEcho(t *testing.T) {
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		return []*Metric{
			NewMetric("test").WithField("value", 1),
		}, nil
	}

	m, err := New("test", collectFn,
		WithEcho(true),
		WithRunOnce(true),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestMonitorShutdown(t *testing.T) {
	collectCount := 0
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		collectCount++
		return []*Metric{
			NewMetric("test").WithField("value", collectCount),
		}, nil
	}

	m, err := New("test", collectFn,
		WithConfig(&Config{
			Global: GlobalConfig{
				PollInterval:  Duration{100 * time.Millisecond},
				LogLevel:      "error",
				BatchSize:     10,
				RetryAttempts: 1,
				RetryDelay:    Duration{1 * time.Millisecond},
			},
		}),
		WithBackend(&mockBackend{name: "test", healthy: true}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	m.Run(ctx)

	// Should have at least 2 collections: initial + at least one from ticker.
	if collectCount < 2 {
		t.Errorf("Expected at least 2 collections, got %d", collectCount)
	}
}

func TestMonitorWithConfigFile(t *testing.T) {
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		return []*Metric{NewMetric("test").WithField("v", 1)}, nil
	}

	_, err := New("test", collectFn,
		WithConfigFile("/nonexistent/config.toml"),
	)
	if err == nil {
		t.Error("New() should error for missing config file")
	}
}

func TestMonitorWithLogger(t *testing.T) {
	collectFn := func(ctx context.Context) ([]*Metric, error) {
		return []*Metric{NewMetric("test").WithField("v", 1)}, nil
	}

	logger, _ := NewLogger("debug")

	m, err := New("test", collectFn,
		WithLogger(logger),
		WithRunOnce(true),
		WithEcho(true),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if m.logger != logger {
		t.Error("Logger should be the one provided via WithLogger")
	}
}
