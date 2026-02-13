package monitor

import (
	"strings"
	"testing"
	"time"
)

func TestNewMetric(t *testing.T) {
	m := NewMetric("cpu")
	if m.Measurement != "cpu" {
		t.Errorf("Measurement = %q, want %q", m.Measurement, "cpu")
	}
	if m.Tags == nil {
		t.Error("Tags should be initialized")
	}
	if m.Fields == nil {
		t.Error("Fields should be initialized")
	}
	if m.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestMetricBuilder(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NewMetric("cpu").
		WithTag("host", "server1").
		WithTags(map[string]string{"region": "us-east"}).
		WithField("usage", 42.5).
		WithFields(map[string]interface{}{"idle": 57.5}).
		WithTimestamp(ts)

	if m.Tags["host"] != "server1" {
		t.Errorf("Tags[host] = %q, want %q", m.Tags["host"], "server1")
	}
	if m.Tags["region"] != "us-east" {
		t.Errorf("Tags[region] = %q, want %q", m.Tags["region"], "us-east")
	}
	if m.Fields["usage"] != 42.5 {
		t.Errorf("Fields[usage] = %v, want %v", m.Fields["usage"], 42.5)
	}
	if m.Fields["idle"] != 57.5 {
		t.Errorf("Fields[idle] = %v, want %v", m.Fields["idle"], 57.5)
	}
	if !m.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", m.Timestamp, ts)
	}
}

func TestMetricClone(t *testing.T) {
	original := NewMetric("cpu").
		WithTag("host", "server1").
		WithField("usage", 42.5)

	clone := original.Clone()

	if clone.Measurement != original.Measurement {
		t.Error("Measurement should match")
	}
	if clone.Tags["host"] != "server1" {
		t.Error("Tags should be copied")
	}

	// Modify clone, verify original is unchanged.
	clone.Tags["host"] = "server2"
	clone.Fields["usage"] = 99.0

	if original.Tags["host"] != "server1" {
		t.Error("Original tags should not be modified")
	}
	if original.Fields["usage"] != 42.5 {
		t.Error("Original fields should not be modified")
	}
}

func TestMetricValidate(t *testing.T) {
	tests := []struct {
		name    string
		metric  *Metric
		wantErr bool
	}{
		{
			name:    "valid metric",
			metric:  NewMetric("cpu").WithField("usage", 42.5),
			wantErr: false,
		},
		{
			name:    "empty measurement",
			metric:  &Metric{Measurement: "", Fields: map[string]interface{}{"a": 1}},
			wantErr: true,
		},
		{
			name:    "no fields",
			metric:  NewMetric("cpu"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.metric.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMetricToLineProtocol(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NewMetric("cpu").
		WithField("usage", 42.5).
		WithTimestamp(ts)

	line := m.ToLineProtocol()
	if !strings.HasPrefix(line, "cpu ") {
		t.Errorf("Line protocol should start with measurement, got %q", line)
	}
	if !strings.Contains(line, "usage=42.5") {
		t.Errorf("Line protocol should contain field, got %q", line)
	}
}

func TestFormatFieldValues(t *testing.T) {
	tests := []struct {
		value interface{}
		want  string
	}{
		{float64(42.5), "42.5"},
		{float32(42.5), "42.5"},
		{int(42), "42i"},
		{int64(42), "42i"},
		{uint(42), "42u"},
		{uint64(42), "42u"},
		{true, "true"},
		{false, "false"},
		{"hello", `"hello"`},
	}

	for _, tt := range tests {
		got := formatFieldValue(tt.value)
		if got != tt.want {
			t.Errorf("formatFieldValue(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestEscapeKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with space", "with\\ space"},
		{"with,comma", "with\\,comma"},
		{"with=equals", "with\\=equals"},
	}

	for _, tt := range tests {
		got := escapeKey(tt.input)
		if got != tt.want {
			t.Errorf("escapeKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
