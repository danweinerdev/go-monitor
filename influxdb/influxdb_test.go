package influxdb

import (
	"testing"

	monitor "github.com/danweinerdev/go-monitor"
)

func TestNewBackend(t *testing.T) {
	cfg := monitor.InfluxDBConfig{
		URL:    "http://localhost:8086",
		Token:  "test-token",
		Org:    "test-org",
		Bucket: "test-bucket",
	}

	b := New(cfg, nil)

	if b.Name() != "influxdb" {
		t.Errorf("Name() = %q, want %q", b.Name(), "influxdb")
	}

	if b.Healthy() {
		t.Error("Backend should not be healthy before Initialize()")
	}
}

func TestBackendWriteNotInitialized(t *testing.T) {
	cfg := monitor.InfluxDBConfig{
		URL:    "http://localhost:8086",
		Token:  "test-token",
		Org:    "test-org",
		Bucket: "test-bucket",
	}

	b := New(cfg, nil)

	metrics := []*monitor.Metric{
		monitor.NewMetric("test").WithField("value", 1),
	}

	err := b.Write(nil, metrics)
	if err == nil {
		t.Error("Write() should fail when not initialized")
	}
}

func TestBackendWriteEmptyBatch(t *testing.T) {
	cfg := monitor.InfluxDBConfig{}
	b := New(cfg, nil)

	err := b.Write(nil, []*monitor.Metric{})
	if err != nil {
		t.Errorf("Write() with empty batch should not error, got %v", err)
	}
}

func TestBackendCloseNilClient(t *testing.T) {
	cfg := monitor.InfluxDBConfig{}
	b := New(cfg, nil)

	if err := b.Close(); err != nil {
		t.Errorf("Close() with nil client should not error, got %v", err)
	}
}
