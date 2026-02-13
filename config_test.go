package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Global.PollInterval.Duration != 10*time.Second {
		t.Errorf("PollInterval = %v, want 10s", cfg.Global.PollInterval.Duration)
	}
	if cfg.Global.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.Global.LogLevel, "info")
	}
	if cfg.Global.BatchSize != 10 {
		t.Errorf("BatchSize = %d, want 10", cfg.Global.BatchSize)
	}
	if cfg.Global.RetryAttempts != 3 {
		t.Errorf("RetryAttempts = %d, want 3", cfg.Global.RetryAttempts)
	}
	if cfg.InfluxDB.Enabled {
		t.Error("InfluxDB should be disabled by default")
	}
	if cfg.Prometheus.Enabled {
		t.Error("Prometheus should be disabled by default")
	}
	if cfg.Prometheus.Port != 9090 {
		t.Errorf("Prometheus.Port = %d, want 9090", cfg.Prometheus.Port)
	}
	if cfg.Prometheus.Path != "/metrics" {
		t.Errorf("Prometheus.Path = %q, want %q", cfg.Prometheus.Path, "/metrics")
	}
}

func TestLoadConfig(t *testing.T) {
	content := `
[global]
poll_interval = "30s"
log_level = "debug"
batch_size = 20
retry_attempts = 5
retry_delay = "2s"

[influxdb]
enabled = true
url = "http://localhost:8086"
token = "my-token"
org = "my-org"
bucket = "my-bucket"

[prometheus]
enabled = true
port = 9191
path = "/metrics"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.Global.PollInterval.Duration != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", cfg.Global.PollInterval.Duration)
	}
	if cfg.Global.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.Global.LogLevel, "debug")
	}
	if cfg.Global.BatchSize != 20 {
		t.Errorf("BatchSize = %d, want 20", cfg.Global.BatchSize)
	}
	if cfg.Global.RetryAttempts != 5 {
		t.Errorf("RetryAttempts = %d, want 5", cfg.Global.RetryAttempts)
	}
	if !cfg.InfluxDB.Enabled {
		t.Error("InfluxDB should be enabled")
	}
	if cfg.InfluxDB.URL != "http://localhost:8086" {
		t.Errorf("InfluxDB.URL = %q, want %q", cfg.InfluxDB.URL, "http://localhost:8086")
	}
	if cfg.InfluxDB.Token != "my-token" {
		t.Errorf("InfluxDB.Token = %q, want %q", cfg.InfluxDB.Token, "my-token")
	}
	if !cfg.Prometheus.Enabled {
		t.Error("Prometheus should be enabled")
	}
	if cfg.Prometheus.Port != 9191 {
		t.Errorf("Prometheus.Port = %d, want 9191", cfg.Prometheus.Port)
	}
}

func TestLoadConfigFromString(t *testing.T) {
	data := `
[global]
poll_interval = "15s"
log_level = "warn"
`
	cfg, err := LoadConfigFromString(data)
	if err != nil {
		t.Fatalf("LoadConfigFromString() error: %v", err)
	}

	if cfg.Global.PollInterval.Duration != 15*time.Second {
		t.Errorf("PollInterval = %v, want 15s", cfg.Global.PollInterval.Duration)
	}
	if cfg.Global.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", cfg.Global.LogLevel, "warn")
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.toml")
	if err == nil {
		t.Error("LoadConfig() should error for missing file")
	}
}

func TestLoadConfigInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("invalid toml [[["), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Error("LoadConfig() should error for invalid TOML")
	}
}

func TestDurationUnmarshalText(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("5s")); err != nil {
		t.Fatalf("UnmarshalText() error: %v", err)
	}
	if d.Duration != 5*time.Second {
		t.Errorf("Duration = %v, want 5s", d.Duration)
	}
}

func TestDurationMarshalText(t *testing.T) {
	d := Duration{30 * time.Second}
	text, err := d.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error: %v", err)
	}
	if string(text) != "30s" {
		t.Errorf("MarshalText() = %q, want %q", string(text), "30s")
	}
}

func TestValidationErrors(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			PollInterval: Duration{0},
			LogLevel:     "invalid",
			BatchSize:    0,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should return error for invalid config")
	}

	errs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("Error should be ValidationErrors, got %T", err)
	}

	if len(errs) < 3 {
		t.Errorf("Expected at least 3 validation errors, got %d", len(errs))
	}
}

func TestValidationInfluxDBRequired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InfluxDB.Enabled = true
	// Leave URL, Token, Org, Bucket empty.

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should error when InfluxDB enabled with empty fields")
	}

	errs := err.(ValidationErrors)
	if len(errs) != 4 {
		t.Errorf("Expected 4 validation errors (url, token, org, bucket), got %d: %v", len(errs), errs)
	}
}

func TestValidationPrometheusRequired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Prometheus.Enabled = true
	cfg.Prometheus.Port = 0
	cfg.Prometheus.Path = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should error when Prometheus enabled with invalid fields")
	}

	errs := err.(ValidationErrors)
	if len(errs) < 2 {
		t.Errorf("Expected at least 2 validation errors, got %d: %v", len(errs), errs)
	}
}

func TestValidationPrometheusPathPrefix(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Prometheus.Enabled = true
	cfg.Prometheus.Port = 9090
	cfg.Prometheus.Path = "metrics" // Missing leading /

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should error when Prometheus path doesn't start with /")
	}
}

func TestValidationDisabledBackendsSkipped(t *testing.T) {
	cfg := DefaultConfig()
	// Both disabled by default, should not validate their fields.
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should pass for default config, got %v", err)
	}
}
