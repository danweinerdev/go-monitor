package monitor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// Backend defines the interface for metric storage backends.
type Backend interface {
	// Name returns the backend name for logging.
	Name() string

	// Initialize sets up the backend connection.
	Initialize(ctx context.Context) error

	// Write sends a batch of metrics to the backend.
	Write(ctx context.Context, metrics []*Metric) error

	// Close cleanly shuts down the backend.
	Close() error

	// Healthy returns true if the backend is operational.
	Healthy() bool
}

// Echo is a debug backend that writes metrics to an io.Writer.
type Echo struct {
	writer io.Writer
	logger *slog.Logger

	mu      sync.RWMutex
	healthy bool
}

// NewEcho creates a new Echo backend that writes to the given writer.
func NewEcho(w io.Writer, logger *slog.Logger) *Echo {
	if w == nil {
		w = os.Stdout
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Echo{
		writer:  w,
		logger:  logger,
		healthy: true,
	}
}

// NewEchoStdout creates an Echo backend that writes to stdout.
func NewEchoStdout(logger *slog.Logger) *Echo {
	return NewEcho(os.Stdout, logger)
}

func (e *Echo) Name() string {
	return "echo"
}

func (e *Echo) Initialize(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.healthy = true
	e.logger.Info("echo backend initialized")
	return nil
}

func (e *Echo) Write(ctx context.Context, batch []*Metric) error {
	e.mu.RLock()
	writer := e.writer
	e.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("echo backend not initialized")
	}

	for _, m := range batch {
		line := m.ToLineProtocol()
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return fmt.Errorf("failed to write metric: %w", err)
		}
	}

	e.logger.Debug("echoed metrics", "count", len(batch))
	return nil
}

func (e *Echo) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.healthy = false
	e.logger.Info("echo backend closed")
	return nil
}

func (e *Echo) Healthy() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.healthy
}

// MultiBackend wraps multiple backends and writes to all of them.
type MultiBackend struct {
	backends []Backend
}

// NewMultiBackend creates a backend that writes to multiple destinations.
func NewMultiBackend(backends ...Backend) *MultiBackend {
	return &MultiBackend{backends: backends}
}

func (m *MultiBackend) Name() string {
	return "multi"
}

func (m *MultiBackend) Initialize(ctx context.Context) error {
	for _, b := range m.backends {
		if err := b.Initialize(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *MultiBackend) Write(ctx context.Context, batch []*Metric) error {
	var lastErr error
	for _, b := range m.backends {
		if err := b.Write(ctx, batch); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (m *MultiBackend) Close() error {
	var lastErr error
	for _, b := range m.backends {
		if err := b.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (m *MultiBackend) Healthy() bool {
	for _, b := range m.backends {
		if !b.Healthy() {
			return false
		}
	}
	return true
}

// Compile-time check that Echo and MultiBackend implement Backend.
var (
	_ Backend = (*Echo)(nil)
	_ Backend = (*MultiBackend)(nil)
)
