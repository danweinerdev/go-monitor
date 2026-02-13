package monitor

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config represents the common monitoring configuration.
type Config struct {
	Global     GlobalConfig     `toml:"global"`
	InfluxDB   InfluxDBConfig   `toml:"influxdb"`
	Prometheus PrometheusConfig `toml:"prometheus"`
}

// GlobalConfig contains global application settings.
type GlobalConfig struct {
	PollInterval  Duration `toml:"poll_interval"`
	LogLevel      string   `toml:"log_level"`
	BatchSize     int      `toml:"batch_size"`
	RetryAttempts int      `toml:"retry_attempts"`
	RetryDelay    Duration `toml:"retry_delay"`
}

// InfluxDBConfig contains InfluxDB connection settings.
type InfluxDBConfig struct {
	Enabled bool   `toml:"enabled"`
	URL     string `toml:"url"`
	Token   string `toml:"token"`
	Org     string `toml:"org"`
	Bucket  string `toml:"bucket"`
}

// PrometheusConfig contains Prometheus exporter settings.
type PrometheusConfig struct {
	Enabled bool   `toml:"enabled"`
	Port    int    `toml:"port"`
	Path    string `toml:"path"`
}

// Duration is a wrapper around time.Duration that supports TOML parsing.
type Duration struct {
	time.Duration
}

// UnmarshalText implements encoding.TextUnmarshaler for Duration.
func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

// MarshalText implements encoding.TextMarshaler for Duration.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Global: GlobalConfig{
			PollInterval:  Duration{10 * time.Second},
			LogLevel:      "info",
			BatchSize:     10,
			RetryAttempts: 3,
			RetryDelay:    Duration{1 * time.Second},
		},
		InfluxDB: InfluxDBConfig{
			Enabled: false,
		},
		Prometheus: PrometheusConfig{
			Enabled: false,
			Port:    9090,
			Path:    "/metrics",
		},
	}
}

// LoadConfig reads and parses a TOML configuration file.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// LoadConfigFromString parses configuration from a TOML string.
func LoadConfigFromString(data string) (*Config, error) {
	cfg := DefaultConfig()

	if err := toml.Unmarshal([]byte(data), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}
