package influxdb

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	monitor "github.com/danweinerdev/go-monitor"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

// Backend implements monitor.Backend for InfluxDB 2.x.
type Backend struct {
	cfg    monitor.InfluxDBConfig
	client influxdb2.Client
	writer api.WriteAPIBlocking
	logger *slog.Logger

	mu      sync.RWMutex
	healthy bool
}

// New creates a new InfluxDB backend.
func New(cfg monitor.InfluxDBConfig, logger *slog.Logger) *Backend {
	if logger == nil {
		logger = slog.Default()
	}
	return &Backend{
		cfg:    cfg,
		logger: logger,
	}
}

func (b *Backend) Name() string {
	return "influxdb"
}

func (b *Backend) Initialize(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.logger.Info("connecting to InfluxDB", "url", b.cfg.URL, "org", b.cfg.Org, "bucket", b.cfg.Bucket)

	opts := influxdb2.DefaultOptions()
	b.client = influxdb2.NewClientWithOptions(b.cfg.URL, b.cfg.Token, opts)

	health, err := b.client.Health(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to InfluxDB: %w", err)
	}

	if health.Status != "pass" {
		return fmt.Errorf("InfluxDB health check failed: %s", health.Status)
	}

	b.writer = b.client.WriteAPIBlocking(b.cfg.Org, b.cfg.Bucket)
	b.healthy = true

	b.logger.Info("connected to InfluxDB", "version", *health.Version)
	return nil
}

func (b *Backend) Write(ctx context.Context, metrics []*monitor.Metric) error {
	// An empty batch is a no-op: do not touch health. Flipping healthy=true
	// here would mark an uninitialized backend healthy without a writer.
	if len(metrics) == 0 {
		return nil
	}

	b.mu.RLock()
	writer := b.writer
	b.mu.RUnlock()

	// An uninitialized backend has no writer: stay unhealthy and error so
	// the pipeline does not treat this as a successful write.
	if writer == nil {
		b.markUnhealthy()
		return fmt.Errorf("InfluxDB not initialized")
	}

	// Force healthy=true on the real write path, before the network call:
	// the pipeline gates on Healthy() before calling Write, so a backend
	// that stays false is never retried. This lets the next scheduled flush
	// re-attempt. If this attempt fails we set healthy back to false below.
	b.mu.Lock()
	b.healthy = true
	b.mu.Unlock()

	points := make([]*write.Point, 0, len(metrics))
	for _, m := range metrics {
		point := influxdb2.NewPoint(
			m.Measurement,
			m.Tags,
			m.Fields,
			m.Timestamp,
		)
		points = append(points, point)
	}

	if err := writer.WritePoint(ctx, points...); err != nil {
		b.markUnhealthy()
		return fmt.Errorf("failed to write to InfluxDB: %w", err)
	}

	b.logger.Debug("wrote metrics to InfluxDB", "count", len(metrics))
	return nil
}

func (b *Backend) markUnhealthy() {
	b.mu.Lock()
	b.healthy = false
	b.mu.Unlock()
}

func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.client != nil {
		b.client.Close()
		b.client = nil
		b.writer = nil
	}
	b.healthy = false

	b.logger.Info("InfluxDB connection closed")
	return nil
}

func (b *Backend) Healthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthy
}

// Compile-time check.
var _ monitor.Backend = (*Backend)(nil)
