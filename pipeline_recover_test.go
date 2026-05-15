package monitor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// recoverFakeBackend follows the real backend health contract: it forces
// healthy=true at the top of every Write (the self-heal the real influxdb and
// elasticsearch backends perform), and on its injected failure it sets
// healthy=false and returns an error (mirroring markUnhealthy()). It fails the
// first Write call, then succeeds on every subsequent call.
type recoverFakeBackend struct {
	mu       sync.Mutex
	healthy  bool
	calls    int
	failFirN int // number of leading Write calls that should fail
}

func (b *recoverFakeBackend) Name() string                         { return "recover-fake" }
func (b *recoverFakeBackend) Initialize(ctx context.Context) error { return nil }

func (b *recoverFakeBackend) Write(ctx context.Context, metrics []*Metric) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Real-contract self-heal: a backend that stays unhealthy is never
	// retried, so Write optimistically flips healthy=true before the work.
	b.healthy = true
	b.calls++

	if b.calls <= b.failFirN {
		// Injected failure: mark unhealthy and return an error, exactly as
		// the real backends do via markUnhealthy().
		b.healthy = false
		return fmt.Errorf("injected write failure on call %d", b.calls)
	}
	return nil
}

func (b *recoverFakeBackend) Close() error { return nil }

func (b *recoverFakeBackend) Healthy() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.healthy
}

func (b *recoverFakeBackend) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// TestPipelineRecoverIntervalProbe is the regression test for the
// health-state deadlock: once a backend goes unhealthy the pipeline must skip
// it only until RecoverInterval elapses, then probe it again so it can
// self-heal. This drives the exported Pipeline.Flush path (Push + Flush, the
// same trigger the other pipeline tests use) and crosses RecoverInterval via
// an injected clock so the test is deterministic and sleep-free.
func TestPipelineRecoverIntervalProbe(t *testing.T) {
	backend := &recoverFakeBackend{healthy: true, failFirN: 1}

	const recover = 30 * time.Second

	cfg := PipelineConfig{
		BatchSize:       100,
		FlushInterval:   1 * time.Hour,
		RetryAttempts:   1, // one Write per flush: deterministic call counting
		RetryDelay:      1 * time.Millisecond,
		RecoverInterval: recover,
	}
	p := NewPipeline(cfg)
	p.AddBackend(backend)

	// Inject a controllable clock. now() must be advanced explicitly; nothing
	// in the pipeline advances it on its own.
	var (
		clockMu sync.Mutex
		current = time.Unix(1_700_000_000, 0)
	)
	p.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return current
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		defer clockMu.Unlock()
		current = current.Add(d)
	}

	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// --- Flush #1: backend is healthy, gets written, fails, goes unhealthy.
	p.Push(NewMetric("cpu").WithField("usage", 1.0))
	if err := p.Flush(ctx); err == nil {
		t.Fatal("Flush #1 should return the injected write error")
	}
	if got := backend.callCount(); got != 1 {
		t.Fatalf("Flush #1: Write call count = %d, want 1", got)
	}
	if backend.Healthy() {
		t.Fatal("Flush #1: backend should be unhealthy after injected failure")
	}

	// --- Flush #2: clock NOT advanced. Backend is unhealthy and was just
	// attempted, so it must be skipped: Write is NOT called again.
	p.Push(NewMetric("cpu").WithField("usage", 2.0))
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush #2 should be a no-op (skipped backend), got error: %v", err)
	}
	if got := backend.callCount(); got != 1 {
		t.Fatalf("Flush #2: Write should NOT be called within RecoverInterval; call count = %d, want 1", got)
	}

	// --- Advance the clock just shy of RecoverInterval: still skipped.
	advance(recover - time.Second)
	p.Push(NewMetric("cpu").WithField("usage", 3.0))
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush #3 should still be skipped before RecoverInterval, got error: %v", err)
	}
	if got := backend.callCount(); got != 1 {
		t.Fatalf("Flush #3: Write should still NOT be called before RecoverInterval; call count = %d, want 1", got)
	}

	// --- Advance past RecoverInterval: the pipeline must probe again. The
	// fake recovers (its Write now succeeds and leaves healthy=true).
	advance(2 * time.Second) // total elapsed since last attempt > RecoverInterval
	p.Push(NewMetric("cpu").WithField("usage", 4.0))
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush #4 (after RecoverInterval) should probe and succeed, got error: %v", err)
	}
	if got := backend.callCount(); got != 2 {
		t.Fatalf("Flush #4: Write should be re-attempted after RecoverInterval; call count = %d, want 2", got)
	}
	if !backend.Healthy() {
		t.Fatal("Flush #4: backend should be healthy again after successful recovery write")
	}

	// --- A further flush with the backend healthy still delivers normally.
	p.Push(NewMetric("cpu").WithField("usage", 5.0))
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush #5 should deliver to the recovered backend, got error: %v", err)
	}
	if got := backend.callCount(); got != 3 {
		t.Fatalf("Flush #5: recovered backend should keep receiving writes; call count = %d, want 3", got)
	}

	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}
