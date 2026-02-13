package promexporter

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	monitor "github.com/danweinerdev/go-monitor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Backend implements monitor.Backend for Prometheus.
// It runs an HTTP server that exposes metrics at the configured path.
type Backend struct {
	cfg       monitor.PrometheusConfig
	collector *dynamicCollector
	server    *http.Server
	logger    *slog.Logger

	mu      sync.RWMutex
	healthy bool
}

// New creates a new Prometheus exporter backend.
func New(cfg monitor.PrometheusConfig, logger *slog.Logger) *Backend {
	if logger == nil {
		logger = slog.Default()
	}
	return &Backend{
		cfg:       cfg,
		collector: newDynamicCollector(),
		logger:    logger,
	}
}

func (b *Backend) Name() string {
	return "prometheus"
}

func (b *Backend) Initialize(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := prometheus.Register(b.collector); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			return fmt.Errorf("failed to register Prometheus collector: %w", err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle(b.cfg.Path, promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", b.cfg.Port)
	b.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		b.logger.Info("starting Prometheus server", "addr", addr, "path", b.cfg.Path)
		if err := b.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			b.logger.Error("Prometheus server error", "error", err)
			b.mu.Lock()
			b.healthy = false
			b.mu.Unlock()
		}
	}()

	b.healthy = true
	return nil
}

func (b *Backend) Write(ctx context.Context, metrics []*monitor.Metric) error {
	b.mu.RLock()
	collector := b.collector
	b.mu.RUnlock()

	if collector == nil {
		return fmt.Errorf("Prometheus not initialized")
	}

	for _, m := range metrics {
		collector.update(m)
	}

	b.logger.Debug("updated Prometheus metrics", "count", len(metrics))
	return nil
}

func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := b.server.Shutdown(ctx); err != nil {
			b.logger.Error("error shutting down Prometheus server", "error", err)
			return err
		}
		b.server = nil
	}

	b.healthy = false
	b.logger.Info("Prometheus server stopped")
	return nil
}

func (b *Backend) Healthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthy
}

// Compile-time check.
var _ monitor.Backend = (*Backend)(nil)

// dynamicCollector is a generic Prometheus collector that dynamically creates
// gauges from metric measurement names and field names.
type dynamicCollector struct {
	mu      sync.RWMutex
	metrics map[string]*metricEntry // keyed by "measurement/tag_values"
}

type metricEntry struct {
	measurement string
	tags        map[string]string
	fields      map[string]float64
}

func newDynamicCollector() *dynamicCollector {
	return &dynamicCollector{
		metrics: make(map[string]*metricEntry),
	}
}

func (c *dynamicCollector) update(m *monitor.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := metricKey(m)
	entry, ok := c.metrics[key]
	if !ok {
		entry = &metricEntry{
			measurement: m.Measurement,
			tags:        make(map[string]string),
			fields:      make(map[string]float64),
		}
		c.metrics[key] = entry
	}

	for k, v := range m.Tags {
		entry.tags[k] = v
	}
	for k, v := range m.Fields {
		entry.fields[k] = toFloat64(v)
	}
}

func metricKey(m *monitor.Metric) string {
	var sb strings.Builder
	sb.WriteString(m.Measurement)

	keys := make([]string, 0, len(m.Tags))
	for k := range m.Tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		sb.WriteByte('/')
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(m.Tags[k])
	}
	return sb.String()
}

func (c *dynamicCollector) Describe(ch chan<- *prometheus.Desc) {
	// Dynamic collector: send nothing from Describe to signal unchecked collector.
}

func (c *dynamicCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, entry := range c.metrics {
		tagKeys := make([]string, 0, len(entry.tags))
		for k := range entry.tags {
			tagKeys = append(tagKeys, k)
		}
		sort.Strings(tagKeys)

		tagValues := make([]string, len(tagKeys))
		for i, k := range tagKeys {
			tagValues[i] = entry.tags[k]
		}

		for fieldName, fieldValue := range entry.fields {
			fqName := sanitizeName(entry.measurement + "_" + fieldName)
			desc := prometheus.NewDesc(fqName, "", tagKeys, nil)
			m, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, fieldValue, tagValues...)
			if err != nil {
				continue
			}
			ch <- m
		}
	}
}

func sanitizeName(s string) string {
	var sb strings.Builder
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
			sb.WriteRune(c)
		case c >= '0' && c <= '9':
			if i == 0 {
				sb.WriteByte('_')
			}
			sb.WriteRune(c)
		default:
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	case bool:
		if val {
			return 1
		}
		return 0
	default:
		return 0
	}
}
