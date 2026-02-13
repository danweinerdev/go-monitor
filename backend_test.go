package monitor

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEchoBackend(t *testing.T) {
	var buf bytes.Buffer
	echo := NewEcho(&buf, nil)

	if echo.Name() != "echo" {
		t.Errorf("Name() = %q, want %q", echo.Name(), "echo")
	}

	if !echo.Healthy() {
		t.Error("Echo should be healthy initially")
	}

	ctx := context.Background()
	if err := echo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	metrics := []*Metric{
		NewMetric("cpu").WithField("usage", 42.5).WithTimestamp(ts),
	}

	if err := echo.Write(ctx, metrics); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cpu") {
		t.Errorf("Output should contain measurement, got %q", output)
	}
	if !strings.Contains(output, "usage=42.5") {
		t.Errorf("Output should contain field, got %q", output)
	}

	if err := echo.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if echo.Healthy() {
		t.Error("Echo should not be healthy after close")
	}
}

func TestEchoStdout(t *testing.T) {
	echo := NewEchoStdout(nil)
	if echo.Name() != "echo" {
		t.Errorf("Name() = %q, want %q", echo.Name(), "echo")
	}
}

type mockBackend struct {
	name        string
	initErr     error
	writeErr    error
	closeErr    error
	healthy     bool
	initialized bool
	written     [][]*Metric
	closed      bool
}

func (m *mockBackend) Name() string { return m.name }
func (m *mockBackend) Initialize(ctx context.Context) error {
	m.initialized = true
	return m.initErr
}
func (m *mockBackend) Write(ctx context.Context, metrics []*Metric) error {
	m.written = append(m.written, metrics)
	return m.writeErr
}
func (m *mockBackend) Close() error {
	m.closed = true
	return m.closeErr
}
func (m *mockBackend) Healthy() bool { return m.healthy }

func TestMultiBackend(t *testing.T) {
	b1 := &mockBackend{name: "b1", healthy: true}
	b2 := &mockBackend{name: "b2", healthy: true}

	multi := NewMultiBackend(b1, b2)

	if multi.Name() != "multi" {
		t.Errorf("Name() = %q, want %q", multi.Name(), "multi")
	}

	ctx := context.Background()
	if err := multi.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}
	if !b1.initialized || !b2.initialized {
		t.Error("Both backends should be initialized")
	}

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	metrics := []*Metric{
		NewMetric("cpu").WithField("usage", 42.5).WithTimestamp(ts),
	}

	if err := multi.Write(ctx, metrics); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if len(b1.written) != 1 || len(b2.written) != 1 {
		t.Error("Both backends should receive the write")
	}

	if !multi.Healthy() {
		t.Error("Multi should be healthy when all backends healthy")
	}

	b1.healthy = false
	if multi.Healthy() {
		t.Error("Multi should be unhealthy when any backend unhealthy")
	}

	if err := multi.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if !b1.closed || !b2.closed {
		t.Error("Both backends should be closed")
	}
}

func TestMultiBackendInitError(t *testing.T) {
	b1 := &mockBackend{name: "b1", initErr: fmt.Errorf("init failed")}
	b2 := &mockBackend{name: "b2", healthy: true}

	multi := NewMultiBackend(b1, b2)

	if err := multi.Initialize(context.Background()); err == nil {
		t.Error("Initialize() should return error")
	}
	if b2.initialized {
		t.Error("b2 should not be initialized when b1 fails")
	}
}

func TestMultiBackendWriteError(t *testing.T) {
	b1 := &mockBackend{name: "b1", healthy: true, writeErr: fmt.Errorf("write failed")}
	b2 := &mockBackend{name: "b2", healthy: true}

	multi := NewMultiBackend(b1, b2)

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	metrics := []*Metric{NewMetric("cpu").WithField("usage", 42.5).WithTimestamp(ts)}

	err := multi.Write(context.Background(), metrics)
	if err == nil {
		t.Error("Write() should return error")
	}
	// b2 should still receive the write even if b1 fails.
	if len(b2.written) != 1 {
		t.Error("b2 should still receive the write")
	}
}
