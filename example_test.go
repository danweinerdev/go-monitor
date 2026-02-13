package monitor_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/danweinerdev/go-monitor"
)

func TestExampleMonitorSmoke(t *testing.T) {
	var buf bytes.Buffer
	echo := monitor.NewEcho(&buf, nil)

	collectFn := func(ctx context.Context) ([]*monitor.Metric, error) {
		return []*monitor.Metric{
			monitor.NewMetric("temperature").
				WithTag("location", "office").
				WithField("celsius", 22.5),
			monitor.NewMetric("humidity").
				WithTag("location", "office").
				WithField("percent", 45.0),
		}, nil
	}

	m, err := monitor.New("example-monitor", collectFn,
		monitor.WithRunOnce(true),
		monitor.WithBackend(echo),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "temperature") {
		t.Errorf("Output should contain 'temperature', got:\n%s", output)
	}
	if !strings.Contains(output, "humidity") {
		t.Errorf("Output should contain 'humidity', got:\n%s", output)
	}
	if !strings.Contains(output, "celsius=22.5") {
		t.Errorf("Output should contain 'celsius=22.5', got:\n%s", output)
	}

	stats := m.Stats()
	if stats.TotalPolls != 1 {
		t.Errorf("TotalPolls = %d, want 1", stats.TotalPolls)
	}
	if stats.SuccessfulPolls != 1 {
		t.Errorf("SuccessfulPolls = %d, want 1", stats.SuccessfulPolls)
	}
	if stats.TotalMetrics != 2 {
		t.Errorf("TotalMetrics = %d, want 2", stats.TotalMetrics)
	}
}
