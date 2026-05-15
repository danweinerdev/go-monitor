package monitor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPipelineBasic(t *testing.T) {
	backend := &mockBackend{name: "test", healthy: true}

	cfg := PipelineConfig{
		BatchSize:     100,
		FlushInterval: 1 * time.Hour,
		RetryAttempts: 1,
		RetryDelay:    1 * time.Millisecond,
	}
	p := NewPipeline(cfg)
	p.AddBackend(backend)

	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if p.BackendCount() != 1 {
		t.Errorf("BackendCount() = %d, want 1", p.BackendCount())
	}

	m := NewMetric("cpu").WithField("usage", 42.5)
	p.Push(m)

	if p.BufferLen() != 1 {
		t.Errorf("BufferLen() = %d, want 1", p.BufferLen())
	}

	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	if len(backend.written) != 1 {
		t.Errorf("Backend should have received 1 write, got %d", len(backend.written))
	}

	if p.BufferLen() != 0 {
		t.Errorf("BufferLen() = %d, want 0 after flush", p.BufferLen())
	}

	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

func TestPipelineInvalidMetricDropped(t *testing.T) {
	backend := &mockBackend{name: "test", healthy: true}

	cfg := PipelineConfig{
		BatchSize:     100,
		FlushInterval: 1 * time.Hour,
		RetryAttempts: 1,
	}
	p := NewPipeline(cfg)
	p.AddBackend(backend)

	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Push invalid metric (no fields).
	p.Push(NewMetric("cpu"))

	if p.BufferLen() != 0 {
		t.Errorf("BufferLen() = %d, want 0 (invalid metric should be dropped)", p.BufferLen())
	}

	p.Stop(ctx)
}

func TestPipelineSkipsUnhealthyBackend(t *testing.T) {
	backend := &mockBackend{name: "test", healthy: false}

	cfg := PipelineConfig{
		BatchSize:       100,
		FlushInterval:   1 * time.Hour,
		RetryAttempts:   1,
		RecoverInterval: 1 * time.Hour,
	}
	p := NewPipeline(cfg)
	p.AddBackend(backend)

	ctx := context.Background()
	p.Start(ctx)

	// First flush: the backend was never attempted, so the pipeline probes
	// it once (this is the recovery attempt that breaks the old skip-forever
	// deadlock). The mock never self-heals, so it stays unhealthy.
	p.Push(NewMetric("cpu").WithField("usage", 42.5))
	p.Flush(ctx)

	if len(backend.written) != 1 {
		t.Errorf("Unhealthy backend should be probed once on first flush, got %d writes", len(backend.written))
	}

	// Second flush within RecoverInterval: the backend is still unhealthy and
	// was just attempted, so it must be skipped (no additional Write).
	p.Push(NewMetric("cpu").WithField("usage", 43.5))
	p.Flush(ctx)

	if len(backend.written) != 1 {
		t.Errorf("Unhealthy backend should be skipped within recovery interval, got %d writes", len(backend.written))
	}

	p.Stop(ctx)
}

func TestPipelineRetry(t *testing.T) {
	callCount := 0
	var mu sync.Mutex
	backend := &retryBackend{
		name:    "test",
		healthy: true,
		writeFn: func(ctx context.Context, metrics []*Metric) error {
			mu.Lock()
			callCount++
			count := callCount
			mu.Unlock()
			if count < 3 {
				return fmt.Errorf("transient error")
			}
			return nil
		},
	}

	cfg := PipelineConfig{
		BatchSize:     100,
		FlushInterval: 1 * time.Hour,
		RetryAttempts: 3,
		RetryDelay:    1 * time.Millisecond,
	}
	p := NewPipeline(cfg)
	p.AddBackend(backend)

	ctx := context.Background()
	p.Start(ctx)

	p.Push(NewMetric("cpu").WithField("usage", 42.5))
	err := p.Flush(ctx)

	if err != nil {
		t.Errorf("Flush() should succeed after retries, got %v", err)
	}

	mu.Lock()
	if callCount != 3 {
		t.Errorf("Expected 3 write attempts, got %d", callCount)
	}
	mu.Unlock()

	p.Stop(ctx)
}

func TestPipelineFlushEmpty(t *testing.T) {
	cfg := DefaultPipelineConfig()
	p := NewPipeline(cfg)

	err := p.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() on empty buffer should not error, got %v", err)
	}
}

func TestPipelineDefaults(t *testing.T) {
	cfg := PipelineConfig{}
	p := NewPipeline(cfg)

	if p.batchSize != 10 {
		t.Errorf("batchSize = %d, want 10", p.batchSize)
	}
	if p.flushInterval != 10*time.Second {
		t.Errorf("flushInterval = %v, want 10s", p.flushInterval)
	}
	if p.retryAttempts != 3 {
		t.Errorf("retryAttempts = %d, want 3", p.retryAttempts)
	}
	if p.retryDelay != 1*time.Second {
		t.Errorf("retryDelay = %v, want 1s", p.retryDelay)
	}
	if p.recoverInterval != 30*time.Second {
		t.Errorf("recoverInterval = %v, want 30s", p.recoverInterval)
	}
}

// retryBackend allows custom write behavior for testing.
type retryBackend struct {
	name    string
	healthy bool
	writeFn func(ctx context.Context, metrics []*Metric) error
}

func (b *retryBackend) Name() string                                 { return b.name }
func (b *retryBackend) Initialize(ctx context.Context) error         { return nil }
func (b *retryBackend) Write(ctx context.Context, m []*Metric) error { return b.writeFn(ctx, m) }
func (b *retryBackend) Close() error                                 { return nil }
func (b *retryBackend) Healthy() bool                                { return b.healthy }
