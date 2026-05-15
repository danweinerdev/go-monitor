package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// PipelineConfig configures the metric pipeline.
type PipelineConfig struct {
	BatchSize       int
	FlushInterval   time.Duration
	RetryAttempts   int
	RetryDelay      time.Duration
	RecoverInterval time.Duration // How often to retry unhealthy backends
	Logger          *slog.Logger
}

// DefaultPipelineConfig returns sensible pipeline defaults.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		BatchSize:       10,
		FlushInterval:   10 * time.Second,
		RetryAttempts:   3,
		RetryDelay:      1 * time.Second,
		RecoverInterval: 30 * time.Second,
		Logger:          slog.Default(),
	}
}

// Pipeline manages metric batching and delivery to backends.
type Pipeline struct {
	backends        []Backend
	batchSize       int
	flushInterval   time.Duration
	retryAttempts   int
	retryDelay      time.Duration
	recoverInterval time.Duration

	mu          sync.Mutex
	buffer      []*Metric
	lastAttempt map[string]time.Time // backend name -> last attempt time
	done        chan struct{}
	wg          sync.WaitGroup
	logger      *slog.Logger

	// now is the clock used for recovery-interval gating. It defaults to
	// time.Now and is overridable in-package by tests for deterministic,
	// sleep-free coverage of shouldAttemptRecovery.
	now func() time.Time
}

// NewPipeline creates a new metric pipeline.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 10 * time.Second
	}
	if cfg.RetryAttempts <= 0 {
		cfg.RetryAttempts = 3
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 1 * time.Second
	}
	if cfg.RecoverInterval <= 0 {
		cfg.RecoverInterval = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Pipeline{
		backends:        make([]Backend, 0),
		batchSize:       cfg.BatchSize,
		flushInterval:   cfg.FlushInterval,
		retryAttempts:   cfg.RetryAttempts,
		retryDelay:      cfg.RetryDelay,
		recoverInterval: cfg.RecoverInterval,
		buffer:          make([]*Metric, 0, cfg.BatchSize),
		lastAttempt:     make(map[string]time.Time),
		done:            make(chan struct{}),
		logger:          cfg.Logger,
		now:             time.Now,
	}
}

// AddBackend adds a backend to the pipeline.
func (p *Pipeline) AddBackend(b Backend) {
	p.backends = append(p.backends, b)
}

// Start begins the background flush goroutine.
func (p *Pipeline) Start(ctx context.Context) error {
	for _, b := range p.backends {
		if err := b.Initialize(ctx); err != nil {
			return err
		}
		p.logger.Info("backend initialized", "backend", b.Name())
	}

	p.wg.Add(1)
	go p.flushLoop(ctx)

	return nil
}

// Stop shuts down the pipeline, flushing remaining metrics.
func (p *Pipeline) Stop(ctx context.Context) error {
	close(p.done)
	p.wg.Wait()

	if err := p.Flush(ctx); err != nil {
		p.logger.Error("final flush failed", "error", err)
	}

	var lastErr error
	for _, b := range p.backends {
		if err := b.Close(); err != nil {
			p.logger.Error("backend close failed", "backend", b.Name(), "error", err)
			lastErr = err
		}
	}

	return lastErr
}

// Push adds a metric to the pipeline.
func (p *Pipeline) Push(m *Metric) {
	if err := m.Validate(); err != nil {
		p.logger.Warn("invalid metric dropped", "error", err)
		return
	}

	p.mu.Lock()
	p.buffer = append(p.buffer, m)
	shouldFlush := len(p.buffer) >= p.batchSize
	p.mu.Unlock()

	if shouldFlush {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := p.Flush(ctx); err != nil {
				p.logger.Error("batch flush failed", "error", err)
			}
		}()
	}
}

// Flush sends all buffered metrics to backends.
func (p *Pipeline) Flush(ctx context.Context) error {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return nil
	}
	batch := p.buffer
	p.buffer = make([]*Metric, 0, p.batchSize)
	p.mu.Unlock()

	p.logger.Debug("flushing metrics", "count", len(batch))

	var lastErr error
	for _, b := range p.backends {
		wasUnhealthy := !b.Healthy()

		if wasUnhealthy {
			// Check if we should attempt recovery. A backend marked
			// unhealthy heals itself at the top of Write, but the pipeline
			// only reaches Write when it decides to probe — gate that probe
			// on the recovery interval so a down backend is re-attempted
			// periodically instead of being skipped forever.
			if !p.shouldAttemptRecovery(b.Name()) {
				p.logger.Debug("skipping unhealthy backend, waiting for recovery interval",
					"backend", b.Name())
				continue
			}
			p.logger.Info("attempting recovery for unhealthy backend", "backend", b.Name())
		}

		p.recordAttempt(b.Name())

		if err := p.writeWithRetry(ctx, b, batch); err != nil {
			p.logger.Error("backend write failed", "backend", b.Name(), "error", err)
			lastErr = err
		}
	}

	return lastErr
}

// shouldAttemptRecovery checks if enough time has passed to retry an unhealthy backend.
func (p *Pipeline) shouldAttemptRecovery(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	last, ok := p.lastAttempt[name]
	if !ok {
		return true // Never attempted, try it
	}
	return p.now().Sub(last) >= p.recoverInterval
}

// recordAttempt records when we last attempted to write to a backend.
func (p *Pipeline) recordAttempt(name string) {
	p.mu.Lock()
	p.lastAttempt[name] = p.now()
	p.mu.Unlock()
}

func (p *Pipeline) writeWithRetry(ctx context.Context, b Backend, metrics []*Metric) error {
	var lastErr error
	for attempt := 1; attempt <= p.retryAttempts; attempt++ {
		err := b.Write(ctx, metrics)
		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < p.retryAttempts {
			p.logger.Warn("write failed, retrying",
				"backend", b.Name(),
				"attempt", attempt,
				"error", err,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(p.retryDelay):
			}
		}
	}
	return lastErr
}

func (p *Pipeline) flushLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Flush(ctx); err != nil {
				p.logger.Error("periodic flush failed", "error", err)
			}
		}
	}
}

// BufferLen returns the current buffer length.
func (p *Pipeline) BufferLen() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.buffer)
}

// BackendCount returns the number of configured backends.
func (p *Pipeline) BackendCount() int {
	return len(p.backends)
}
