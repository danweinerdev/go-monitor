package monitor

import (
	"fmt"
	"strings"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "no errors"
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Error()
	}
	return fmt.Sprintf("multiple validation errors:\n  - %s", strings.Join(msgs, "\n  - "))
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	var errs ValidationErrors

	errs = append(errs, c.validateGlobal()...)
	errs = append(errs, c.validateInfluxDB()...)
	errs = append(errs, c.validatePrometheus()...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func (c *Config) validateGlobal() ValidationErrors {
	var errs ValidationErrors

	if c.Global.PollInterval.Duration <= 0 {
		errs = append(errs, ValidationError{
			Field:   "global.poll_interval",
			Message: "must be positive",
		})
	}

	if c.Global.BatchSize <= 0 {
		errs = append(errs, ValidationError{
			Field:   "global.batch_size",
			Message: "must be positive",
		})
	}

	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLevels[strings.ToLower(c.Global.LogLevel)] {
		errs = append(errs, ValidationError{
			Field:   "global.log_level",
			Message: "must be one of: debug, info, warn, error",
		})
	}

	return errs
}

func (c *Config) validateInfluxDB() ValidationErrors {
	var errs ValidationErrors

	if !c.InfluxDB.Enabled {
		return errs
	}

	if c.InfluxDB.URL == "" {
		errs = append(errs, ValidationError{
			Field:   "influxdb.url",
			Message: "required when InfluxDB is enabled",
		})
	}

	if c.InfluxDB.Token == "" {
		errs = append(errs, ValidationError{
			Field:   "influxdb.token",
			Message: "required when InfluxDB is enabled",
		})
	}

	if c.InfluxDB.Org == "" {
		errs = append(errs, ValidationError{
			Field:   "influxdb.org",
			Message: "required when InfluxDB is enabled",
		})
	}

	if c.InfluxDB.Bucket == "" {
		errs = append(errs, ValidationError{
			Field:   "influxdb.bucket",
			Message: "required when InfluxDB is enabled",
		})
	}

	return errs
}

func (c *Config) validatePrometheus() ValidationErrors {
	var errs ValidationErrors

	if !c.Prometheus.Enabled {
		return errs
	}

	if c.Prometheus.Port <= 0 || c.Prometheus.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "prometheus.port",
			Message: "must be a valid port (1-65535)",
		})
	}

	if c.Prometheus.Path == "" {
		errs = append(errs, ValidationError{
			Field:   "prometheus.path",
			Message: "required when Prometheus is enabled",
		})
	}

	if c.Prometheus.Path != "" && !strings.HasPrefix(c.Prometheus.Path, "/") {
		errs = append(errs, ValidationError{
			Field:   "prometheus.path",
			Message: "must start with /",
		})
	}

	return errs
}
