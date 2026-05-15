package monitor

import (
	"context"
	"testing"
	"time"
)

// recordingBackend is a fake Backend that records every method invocation so
// delegation behavior can be asserted.
type recordingBackend struct {
	name string

	initCalls    int
	closeCalls   int
	healthyCalls int
	writeCalls   int
	writeBatches [][]*Metric

	healthy  bool
	writeErr error
}

func (r *recordingBackend) Name() string { return r.name }

func (r *recordingBackend) Initialize(ctx context.Context) error {
	r.initCalls++
	return nil
}

func (r *recordingBackend) Write(ctx context.Context, metrics []*Metric) error {
	r.writeCalls++
	r.writeBatches = append(r.writeBatches, metrics)
	return r.writeErr
}

func (r *recordingBackend) Close() error {
	r.closeCalls++
	return nil
}

func (r *recordingBackend) Healthy() bool {
	r.healthyCalls++
	return r.healthy
}

func TestMeasurementFilterMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
	}{
		{"exact match hit", "cpu", "cpu", true},
		{"exact match miss", "cpu", "cpux", false},
		{"exact match miss case-sensitive", "cpu", "CPU", false},
		{"exact match miss prefix-only", "cpu", "cp", false},
		{"trailing-* prefix hit", "net*", "network", true},
		{"trailing-* prefix hit exact prefix", "net*", "net", true},
		{"trailing-* prefix miss", "net*", "disk", false},
		{"trailing-* prefix miss case-sensitive", "net*", "NETwork", false},
		{"bare * matches everything", "*", "anything", true},
		{"bare * matches empty", "*", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchPattern(tt.pattern, tt.input); got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.input, got, tt.want)
			}
		})
	}
}

func TestMeasurementFilterValidate(t *testing.T) {
	tests := []struct {
		name    string
		include []string
		exclude []string
		wantErr bool
	}{
		{"empty is valid", nil, nil, false},
		{"literal is valid", []string{"cpu"}, nil, false},
		{"trailing-* is valid", []string{"net*"}, nil, false},
		{"bare * is valid", []string{"*"}, nil, false},
		{"multiple valid patterns", []string{"cpu", "net*", "*"}, []string{"disk*"}, false},
		{"leading-* is invalid", []string{"*cpu"}, nil, true},
		{"mid-string-* is invalid", []string{"ne*work"}, nil, true},
		{"multiple-* is invalid", []string{"net**"}, nil, true},
		{"multiple-* separated is invalid", []string{"a*b*"}, nil, true},
		{"invalid in exclude only", nil, []string{"*disk"}, true},
		{"valid include but invalid exclude", []string{"cpu"}, []string{"di*sk"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &MeasurementFilter{
				Inner:   &recordingBackend{name: "rec"},
				Include: tt.include,
				Exclude: tt.exclude,
			}
			err := f.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestMeasurementFilterValidateNamesPattern(t *testing.T) {
	f := &MeasurementFilter{
		Inner:   &recordingBackend{name: "rec"},
		Exclude: []string{"good*", "ba*d"},
	}
	err := f.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	if got := err.Error(); !contains(got, "ba*d") {
		t.Errorf("Validate() error = %q, want it to name the offending pattern %q", got, "ba*d")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestMeasurementFilterOutcomeRules(t *testing.T) {
	tests := []struct {
		name    string
		include []string
		exclude []string
		input   string
		want    bool
	}{
		{"empty include + empty exclude passes", nil, nil, "anything", true},
		{"include-only hit", []string{"cpu", "net*"}, nil, "network", true},
		{"include-only miss", []string{"cpu", "net*"}, nil, "disk", false},
		{"exclude-only drop", nil, []string{"disk*"}, "disk0", false},
		{"exclude-only pass", nil, []string{"disk*"}, "cpu", true},
		{"both: matches include, not exclude -> pass", []string{"net*"}, []string{"netfilter"}, "network", true},
		{"both: matches include and exclude -> drop", []string{"net*"}, []string{"netfilter"}, "netfilter", false},
		{"both: no include match -> drop", []string{"net*"}, []string{"netfilter"}, "cpu", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &MeasurementFilter{
				Inner:   &recordingBackend{name: "rec"},
				Include: tt.include,
				Exclude: tt.exclude,
			}
			if got := f.allows(tt.input); got != tt.want {
				t.Errorf("allows(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMeasurementFilterName(t *testing.T) {
	f := &MeasurementFilter{Inner: &recordingBackend{name: "echo"}}
	if got, want := f.Name(), "filter(echo)"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestMeasurementFilterDelegation(t *testing.T) {
	rec := &recordingBackend{name: "echo", healthy: true}
	f := &MeasurementFilter{Inner: rec}

	ctx := context.Background()

	if err := f.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}
	if rec.initCalls != 1 {
		t.Errorf("Inner.Initialize called %d times, want 1", rec.initCalls)
	}

	if !f.Healthy() {
		t.Error("Healthy() = false, want true (delegated)")
	}
	if rec.healthyCalls != 1 {
		t.Errorf("Inner.Healthy called %d times, want 1", rec.healthyCalls)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if rec.closeCalls != 1 {
		t.Errorf("Inner.Close called %d times, want 1", rec.closeCalls)
	}
}

func TestMeasurementFilterWriteForwardsSurvivors(t *testing.T) {
	rec := &recordingBackend{name: "echo", healthy: true}
	f := &MeasurementFilter{
		Inner:   rec,
		Include: []string{"cpu", "net*"},
	}

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	batch := []*Metric{
		NewMetric("cpu").WithField("v", 1).WithTimestamp(ts),
		NewMetric("disk").WithField("v", 2).WithTimestamp(ts),
		NewMetric("network").WithField("v", 3).WithTimestamp(ts),
	}

	if err := f.Write(context.Background(), batch); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	if rec.writeCalls != 1 {
		t.Fatalf("Inner.Write called %d times, want 1", rec.writeCalls)
	}
	got := rec.writeBatches[0]
	if len(got) != 2 {
		t.Fatalf("forwarded %d metrics, want 2", len(got))
	}
	if got[0].Measurement != "cpu" || got[1].Measurement != "network" {
		t.Errorf("forwarded measurements = [%q, %q], want [cpu, network]",
			got[0].Measurement, got[1].Measurement)
	}
}

func TestMeasurementFilterWriteEmptyResultSkipsInner(t *testing.T) {
	rec := &recordingBackend{name: "echo", healthy: true}
	f := &MeasurementFilter{
		Inner:   rec,
		Include: []string{"cpu"},
	}

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	batch := []*Metric{
		NewMetric("disk").WithField("v", 1).WithTimestamp(ts),
		NewMetric("network").WithField("v", 2).WithTimestamp(ts),
	}

	if err := f.Write(context.Background(), batch); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	if rec.writeCalls != 0 {
		t.Errorf("Inner.Write called %d times, want 0 (all metrics filtered out)", rec.writeCalls)
	}
}

func TestMeasurementFilterWriteEmptyBatchSkipsInner(t *testing.T) {
	rec := &recordingBackend{name: "echo", healthy: true}
	f := &MeasurementFilter{Inner: rec}

	if err := f.Write(context.Background(), nil); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if rec.writeCalls != 0 {
		t.Errorf("Inner.Write called %d times, want 0 (empty batch)", rec.writeCalls)
	}
}
