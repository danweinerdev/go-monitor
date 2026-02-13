package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// CollectFunc is the user-provided function that collects metrics.
type CollectFunc func(ctx context.Context) ([]*Metric, error)

// Monitor is the core runtime that handles polling, signals, backends, and shutdown.
type Monitor struct {
	name      string
	collector CollectFunc
	pipeline  *Pipeline
	signals   *SignalHandler
	logger    *slog.Logger
	levelVar  *slog.LevelVar
	cfg       *Config
	cfgPath   string
	echoMode  bool
	runOnce   bool
	reloadFn  func(string) (*Config, error)
	backends  []Backend
	stats     statsTracker
}

// New creates a new Monitor with the given name, collect function, and options.
func New(name string, collector CollectFunc, opts ...Option) (*Monitor, error) {
	m := &Monitor{
		name:      name,
		collector: collector,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Load config from file if path given and no config provided directly.
	if m.cfg == nil && m.cfgPath != "" {
		cfg, err := LoadConfig(m.cfgPath)
		if err != nil {
			return nil, fmt.Errorf("loading config: %w", err)
		}
		m.cfg = cfg
	}

	// Apply defaults if no config at all.
	if m.cfg == nil {
		m.cfg = DefaultConfig()
	}

	// Set up logger if not provided.
	if m.logger == nil {
		m.logger, m.levelVar = NewLogger(m.cfg.Global.LogLevel)
	}

	return m, nil
}

// Run starts the monitor and blocks until shutdown.
func (m *Monitor) Run(ctx context.Context) error {
	m.logger.Info("starting monitor",
		"name", m.name,
		"interval", m.cfg.Global.PollInterval.Duration,
	)

	// Build pipeline.
	pipelineCfg := PipelineConfig{
		BatchSize:     m.cfg.Global.BatchSize,
		FlushInterval: m.cfg.Global.PollInterval.Duration,
		RetryAttempts: m.cfg.Global.RetryAttempts,
		RetryDelay:    m.cfg.Global.RetryDelay.Duration,
		Logger:        m.logger,
	}
	m.pipeline = NewPipeline(pipelineCfg)

	// Add backends.
	if err := m.addBackends(); err != nil {
		return err
	}

	// Start pipeline (initializes backends).
	if err := m.pipeline.Start(ctx); err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.pipeline.Stop(stopCtx); err != nil {
			m.logger.Error("error stopping pipeline", "error", err)
		}
	}()

	// Set up signal handler.
	m.signals = NewSignalHandler(m.logger)
	ctx = m.signals.Start(ctx)

	// Immediate first collection.
	m.collect(ctx)

	if m.runOnce {
		return nil
	}

	// Main polling loop.
	ticker := time.NewTicker(m.cfg.Global.PollInterval.Duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("shutting down, performing final collection...")
			finalCtx, finalCancel := context.WithTimeout(context.Background(), 30*time.Second)
			m.collect(finalCtx)
			finalCancel()
			m.logger.Info("shutdown complete")
			return nil

		case <-m.signals.Reload():
			m.handleReload(ticker)

		case <-ticker.C:
			m.collect(ctx)
		}
	}
}

// Stats returns a snapshot of polling statistics.
func (m *Monitor) Stats() PollStats {
	return m.stats.snapshot()
}

func (m *Monitor) addBackends() error {
	// Add user-provided backends first.
	for _, b := range m.backends {
		m.pipeline.AddBackend(b)
	}

	if m.echoMode {
		m.pipeline.AddBackend(NewEchoStdout(m.logger))
	} else {
		// InfluxDB and Prometheus are handled by sub-packages;
		// the user adds them via WithBackend.
		// If no backends configured at all, add echo as fallback.
	}

	if m.pipeline.BackendCount() == 0 {
		return fmt.Errorf("no backends configured (use WithBackend, WithEcho, or enable backends in config)")
	}

	return nil
}

func (m *Monitor) collect(ctx context.Context) {
	start := time.Now()

	metrics, err := m.collector(ctx)
	duration := time.Since(start)

	if err != nil {
		m.stats.recordPoll(false, 0, duration)
		m.logger.Error("collection failed", "error", err, "duration", duration)
		return
	}

	m.stats.recordPoll(true, len(metrics), duration)

	for _, metric := range metrics {
		m.pipeline.Push(metric)
	}

	m.logger.Info("poll completed",
		"metrics", len(metrics),
		"duration", duration,
	)
}

func (m *Monitor) handleReload(ticker *time.Ticker) {
	m.logger.Info("reloading configuration")

	var newCfg *Config
	var err error

	if m.reloadFn != nil {
		newCfg, err = m.reloadFn(m.cfgPath)
	} else if m.cfgPath != "" {
		newCfg, err = LoadConfig(m.cfgPath)
	} else {
		m.logger.Warn("no config path or reload function, ignoring reload signal")
		return
	}

	if err != nil {
		m.logger.Error("config reload failed, keeping current config", "error", err)
		return
	}

	// Update poll interval if changed.
	if newCfg.Global.PollInterval.Duration != m.cfg.Global.PollInterval.Duration {
		ticker.Reset(newCfg.Global.PollInterval.Duration)
		m.logger.Info("updated poll interval", "interval", newCfg.Global.PollInterval.Duration)
	}

	// Update log level if changed.
	if m.levelVar != nil && newCfg.Global.LogLevel != m.cfg.Global.LogLevel {
		m.levelVar.Set(ParseLogLevel(newCfg.Global.LogLevel))
		m.logger.Info("updated log level", "level", newCfg.Global.LogLevel)
	}

	m.cfg = newCfg
}
