package monitor

import (
	"fmt"
	"strings"
	"time"
)

// Metric represents a single data point to be sent to backends.
type Metric struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]interface{}
	Timestamp   time.Time
}

// NewMetric creates a new Metric with the given measurement name.
func NewMetric(measurement string) *Metric {
	return &Metric{
		Measurement: measurement,
		Tags:        make(map[string]string),
		Fields:      make(map[string]interface{}),
		Timestamp:   time.Now(),
	}
}

// WithTags adds multiple tags to the metric.
func (m *Metric) WithTags(tags map[string]string) *Metric {
	for k, v := range tags {
		m.Tags[k] = v
	}
	return m
}

// WithTag adds a single tag to the metric.
func (m *Metric) WithTag(key, value string) *Metric {
	m.Tags[key] = value
	return m
}

// WithFields adds multiple fields to the metric.
func (m *Metric) WithFields(fields map[string]interface{}) *Metric {
	for k, v := range fields {
		m.Fields[k] = v
	}
	return m
}

// WithField adds a single field to the metric.
func (m *Metric) WithField(key string, value interface{}) *Metric {
	m.Fields[key] = value
	return m
}

// WithTimestamp sets the metric timestamp.
func (m *Metric) WithTimestamp(t time.Time) *Metric {
	m.Timestamp = t
	return m
}

// Clone creates a deep copy of the metric.
func (m *Metric) Clone() *Metric {
	clone := &Metric{
		Measurement: m.Measurement,
		Tags:        make(map[string]string, len(m.Tags)),
		Fields:      make(map[string]interface{}, len(m.Fields)),
		Timestamp:   m.Timestamp,
	}
	for k, v := range m.Tags {
		clone.Tags[k] = v
	}
	for k, v := range m.Fields {
		clone.Fields[k] = v
	}
	return clone
}

// Validate checks if the metric is valid for sending.
func (m *Metric) Validate() error {
	if m.Measurement == "" {
		return fmt.Errorf("measurement name is required")
	}
	if len(m.Fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}
	return nil
}

// ToLineProtocol converts the metric to InfluxDB line protocol format.
func (m *Metric) ToLineProtocol() string {
	var sb strings.Builder

	sb.WriteString(escapeKey(m.Measurement))

	for k, v := range m.Tags {
		sb.WriteByte(',')
		sb.WriteString(escapeKey(k))
		sb.WriteByte('=')
		sb.WriteString(escapeTagValue(v))
	}

	sb.WriteByte(' ')

	first := true
	for k, v := range m.Fields {
		if !first {
			sb.WriteByte(',')
		}
		first = false
		sb.WriteString(escapeKey(k))
		sb.WriteByte('=')
		sb.WriteString(formatFieldValue(v))
	}

	sb.WriteByte(' ')
	sb.WriteString(fmt.Sprintf("%d", m.Timestamp.UnixNano()))

	return sb.String()
}

func escapeKey(s string) string {
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "=", "\\=")
	s = strings.ReplaceAll(s, " ", "\\ ")
	return s
}

func escapeTagValue(s string) string {
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "=", "\\=")
	s = strings.ReplaceAll(s, " ", "\\ ")
	return s
}

func formatFieldValue(v interface{}) string {
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%g", val)
	case float32:
		return fmt.Sprintf("%g", val)
	case int:
		return fmt.Sprintf("%di", val)
	case int8:
		return fmt.Sprintf("%di", val)
	case int16:
		return fmt.Sprintf("%di", val)
	case int32:
		return fmt.Sprintf("%di", val)
	case int64:
		return fmt.Sprintf("%di", val)
	case uint:
		return fmt.Sprintf("%du", val)
	case uint8:
		return fmt.Sprintf("%du", val)
	case uint16:
		return fmt.Sprintf("%du", val)
	case uint32:
		return fmt.Sprintf("%du", val)
	case uint64:
		return fmt.Sprintf("%du", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case string:
		return fmt.Sprintf("%q", val)
	default:
		return fmt.Sprintf("%q", fmt.Sprint(val))
	}
}
