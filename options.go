package monitor

import "log/slog"

// Option configures a Monitor.
type Option func(*Monitor)

// WithConfigFile sets the path to a TOML config file.
func WithConfigFile(path string) Option {
	return func(m *Monitor) {
		m.cfgPath = path
	}
}

// WithConfig provides a Config directly instead of loading from file.
func WithConfig(cfg *Config) Option {
	return func(m *Monitor) {
		m.cfg = cfg
	}
}

// WithEcho enables echo mode (metrics to stdout).
func WithEcho(enabled bool) Option {
	return func(m *Monitor) {
		m.echoMode = enabled
	}
}

// WithRunOnce runs a single collection cycle and exits.
func WithRunOnce(enabled bool) Option {
	return func(m *Monitor) {
		m.runOnce = enabled
	}
}

// WithLogger provides a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(m *Monitor) {
		m.logger = logger
	}
}

// WithBackend adds a custom backend.
func WithBackend(b Backend) Option {
	return func(m *Monitor) {
		m.backends = append(m.backends, b)
	}
}

// WithReloadFunc provides a custom config reload function.
// The function receives the config file path and returns a new Config.
func WithReloadFunc(fn func(path string) (*Config, error)) Option {
	return func(m *Monitor) {
		m.reloadFn = fn
	}
}
