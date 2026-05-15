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
// healthy=false and returns an error (mirroring markUnhealthy()). It fails its
// first failFirstN Write calls, then succeeds on every subsequent call.
type recoverFakeBackend struct {
	mu         sync.Mutex
	healthy    bool
	calls      int
	failFirstN int // number of leading Write calls that should fail
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

	if b.calls <= b.failFirstN {
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
// health-state deadlock: once a backend goes unhealthy the pipeline must
// probe it (immediately on the first post-failure flush, which starts the
// cooldown clock), then skip it until RecoverInterval elapses, then probe it
// again so it can self-heal. This drives the exported Pipeline.Flush path
// (Push + Flush, the same trigger the other pipeline tests use) and crosses
// RecoverInterval via an injected clock so the test is deterministic and
// sleep-free.
func TestPipelineRecoverIntervalProbe(t *testing.T) {
	// failFirstN: 2 so the first two Write calls fail. Call #1 is the healthy
	// flush that knocks the backend unhealthy; call #2 is the immediate
	// first post-failure recovery probe (which under the corrected contract
	// happens on the very next flush and starts the cooldown clock). The
	// backend recovers on call #3.
	backend := &recoverFakeBackend{healthy: true, failFirstN: 2}

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

	// --- Flush #2: clock NOT advanced. Backend is unhealthy; this is the
	// FIRST flush since it went unhealthy, so it is also the first recovery
	// probe. Under the corrected contract (the probe timestamp is recorded
	// only on an actual unhealthy-backend probe, never on a healthy flush)
	// that first post-failure probe happens immediately and starts the
	// cooldown clock here — it is NOT skipped. The probe fails again
	// (failFirstN=2), so the backend stays unhealthy.
	p.Push(NewMetric("cpu").WithField("usage", 2.0))
	if err := p.Flush(ctx); err == nil {
		t.Fatal("Flush #2 (first post-failure probe) should run and return the injected write error")
	}
	if got := backend.callCount(); got != 2 {
		t.Fatalf("Flush #2: first post-failure flush must probe immediately; call count = %d, want 2", got)
	}
	if backend.Healthy() {
		t.Fatal("Flush #2: backend should still be unhealthy after the failing recovery probe")
	}

	// --- Advance the clock just shy of RecoverInterval: now the cooldown
	// (started by the Flush #2 probe) gates the backend, so it is skipped.
	advance(recover - time.Second)
	p.Push(NewMetric("cpu").WithField("usage", 3.0))
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush #3 should be skipped before RecoverInterval elapses, got error: %v", err)
	}
	if got := backend.callCount(); got != 2 {
		t.Fatalf("Flush #3: Write should NOT be called before RecoverInterval; call count = %d, want 2", got)
	}

	// --- Advance past RecoverInterval: the pipeline must probe again. The
	// fake recovers (call #3 succeeds and leaves healthy=true).
	advance(2 * time.Second) // total elapsed since the Flush #2 probe > RecoverInterval
	p.Push(NewMetric("cpu").WithField("usage", 4.0))
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush #4 (after RecoverInterval) should probe and succeed, got error: %v", err)
	}
	if got := backend.callCount(); got != 3 {
		t.Fatalf("Flush #4: Write should be re-attempted after RecoverInterval; call count = %d, want 3", got)
	}
	if !backend.Healthy() {
		t.Fatal("Flush #4: backend should be healthy again after successful recovery write")
	}

	// --- A further flush with the backend healthy still delivers normally.
	p.Push(NewMetric("cpu").WithField("usage", 5.0))
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("Flush #5 should deliver to the recovered backend, got error: %v", err)
	}
	if got := backend.callCount(); got != 4 {
		t.Fatalf("Flush #5: recovered backend should keep receiving writes; call count = %d, want 4", got)
	}

	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}
