package promexporter

import (
	"testing"

	monitor "github.com/danweinerdev/go-monitor"
	"github.com/prometheus/client_golang/prometheus"
)

func TestNewBackend(t *testing.T) {
	cfg := monitor.PrometheusConfig{
		Port: 9999,
		Path: "/metrics",
	}

	b := New(cfg, nil)

	if b.Name() != "prometheus" {
		t.Errorf("Name() = %q, want %q", b.Name(), "prometheus")
	}

	if b.Healthy() {
		t.Error("Backend should not be healthy before Initialize()")
	}
}

func TestDynamicCollector(t *testing.T) {
	c := newDynamicCollector()

	m := monitor.NewMetric("cpu").
		WithTag("host", "server1").
		WithField("usage", 42.5).
		WithField("idle", 57.5)

	c.update(m)

	// Collect metrics.
	ch := make(chan prometheus.Metric, 10)
	c.Collect(ch)
	close(ch)

	var collected []prometheus.Metric
	for m := range ch {
		collected = append(collected, m)
	}

	if len(collected) != 2 {
		t.Errorf("Expected 2 prometheus metrics, got %d", len(collected))
	}
}

func TestDynamicCollectorUpdate(t *testing.T) {
	c := newDynamicCollector()

	// First update.
	m1 := monitor.NewMetric("cpu").
		WithTag("host", "server1").
		WithField("usage", 42.5)
	c.update(m1)

	// Second update with new value.
	m2 := monitor.NewMetric("cpu").
		WithTag("host", "server1").
		WithField("usage", 80.0)
	c.update(m2)

	// Should have one entry (same key).
	if len(c.metrics) != 1 {
		t.Errorf("Expected 1 metric entry, got %d", len(c.metrics))
	}

	// Value should be updated.
	for _, entry := range c.metrics {
		if entry.fields["usage"] != 80.0 {
			t.Errorf("usage = %v, want 80.0", entry.fields["usage"])
		}
	}
}

func TestDynamicCollectorMultipleDevices(t *testing.T) {
	c := newDynamicCollector()

	m1 := monitor.NewMetric("cpu").
		WithTag("host", "server1").
		WithField("usage", 42.5)
	m2 := monitor.NewMetric("cpu").
		WithTag("host", "server2").
		WithField("usage", 80.0)

	c.update(m1)
	c.update(m2)

	if len(c.metrics) != 2 {
		t.Errorf("Expected 2 metric entries, got %d", len(c.metrics))
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with-dash", "with_dash"},
		{"with.dot", "with_dot"},
		{"with space", "with_space"},
		{"123start", "_123start"},
		{"UPPER_case", "UPPER_case"},
	}

	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input interface{}
		want  float64
	}{
		{float64(42.5), 42.5},
		{float32(42.5), float64(float32(42.5))},
		{int(42), 42.0},
		{int64(42), 42.0},
		{uint(42), 42.0},
		{true, 1.0},
		{false, 0.0},
		{"not a number", 0.0},
	}

	for _, tt := range tests {
		got := toFloat64(tt.input)
		if got != tt.want {
			t.Errorf("toFloat64(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestMetricKey(t *testing.T) {
	m := monitor.NewMetric("cpu").
		WithTag("host", "server1").
		WithTag("region", "us-east")

	key := metricKey(m)

	// Tags should be sorted.
	if key != "cpu/host=server1/region=us-east" {
		t.Errorf("metricKey() = %q, want %q", key, "cpu/host=server1/region=us-east")
	}
}
