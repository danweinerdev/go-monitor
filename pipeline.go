package monitor

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// PipelineConfig configures the metric pipeline.
type PipelineConfig struct {
	BatchSize     int
	FlushInterval time.Duration
	RetryAttempts int
	RetryDelay    time.Duration
	Logger        *slog.Logger
}

// DefaultPipelineConfig returns sensible pipeline defaults.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		BatchSize:     10,
		FlushInterval: 10 * time.Second,
		RetryAttempts: 3,
		RetryDelay:    1 * time.Second,
		Logger:        slog.Default(),
	}
}

// Pipeline manages metric batching and delivery to backends.
type Pipeline struct {
	backends      []Backend
	batchSize     int
	flushInterval time.Duration
	retryAttempts int
	retryDelay    time.Duration

	mu     sync.Mutex
	buffer []*Metric
	done   chan struct{}
	wg     sync.WaitGroup
	logger *slog.Logger
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
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Pipeline{
		backends:      make([]Backend, 0),
		batchSize:     cfg.BatchSize,
		flushInterval: cfg.FlushInterval,
		retryAttempts: cfg.RetryAttempts,
		retryDelay:    cfg.RetryDelay,
		buffer:        make([]*Metric, 0, cfg.BatchSize),
		done:          make(chan struct{}),
		logger:        cfg.Logger,
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
		if !b.Healthy() {
			p.logger.Warn("skipping unhealthy backend", "backend", b.Name())
			continue
		}

		if err := p.writeWithRetry(ctx, b, batch); err != nil {
			p.logger.Error("backend write failed", "backend", b.Name(), "error", err)
			lastErr = err
		}
	}

	return lastErr
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
