package monitor

import (
	"context"
	"fmt"
	"strings"
)

// MeasurementFilter wraps an inner Backend and filters metrics by measurement
// name before forwarding them, enabling per-backend measurement routing.
//
// Include and Exclude are lists of patterns. A pattern is either a literal
// (no '*'), matched byte-for-byte against the measurement name, or a prefix
// pattern whose only '*' is its final character, matched with strings.HasPrefix
// against the measurement name. The pattern "*" alone matches everything.
// Matching is case-sensitive. Any other use of '*' (leading, mid-string, or
// multiple) is a configuration error reported by Validate.
type MeasurementFilter struct {
	Inner   Backend
	Include []string
	Exclude []string
}

// Name returns the filtered backend name for logging.
func (f *MeasurementFilter) Name() string {
	return "filter(" + f.Inner.Name() + ")"
}

// Initialize delegates to the inner backend.
func (f *MeasurementFilter) Initialize(ctx context.Context) error {
	return f.Inner.Initialize(ctx)
}

// Write filters the batch by measurement name and forwards the survivors to
// the inner backend. If nothing survives the filter, the inner backend is not
// called at all.
func (f *MeasurementFilter) Write(ctx context.Context, batch []*Metric) error {
	filtered := make([]*Metric, 0, len(batch))
	for _, m := range batch {
		if f.allows(m.Measurement) {
			filtered = append(filtered, m)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	return f.Inner.Write(ctx, filtered)
}

// Close delegates to the inner backend.
func (f *MeasurementFilter) Close() error {
	return f.Inner.Close()
}

// Healthy delegates to the inner backend.
func (f *MeasurementFilter) Healthy() bool {
	return f.Inner.Healthy()
}

// Validate checks that every Include and Exclude pattern is well-formed. A
// pattern is well-formed if it contains no '*', or if its only '*' is the
// final character. It returns a non-nil error naming the first offending
// pattern otherwise.
func (f *MeasurementFilter) Validate() error {
	for _, p := range f.Include {
		if err := validatePattern(p); err != nil {
			return fmt.Errorf("invalid include pattern %q: %w", p, err)
		}
	}
	for _, p := range f.Exclude {
		if err := validatePattern(p); err != nil {
			return fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
	}
	return nil
}

// validatePattern reports whether p is a well-formed literal or trailing-'*'
// prefix pattern.
func validatePattern(p string) error {
	idx := strings.IndexByte(p, '*')
	if idx == -1 {
		return nil
	}
	// The first '*' must be the final character, and there must be no others.
	if idx != len(p)-1 {
		return fmt.Errorf("'*' is only allowed as the final character")
	}
	return nil
}

// matchPattern reports whether a single well-formed pattern matches name.
// Behavior on a malformed pattern is unspecified; callers should run Validate
// to reject such patterns at configuration time.
func matchPattern(pattern, name string) bool {
	if strings.IndexByte(pattern, '*') == -1 {
		return pattern == name
	}
	// Trailing-'*' prefix pattern: everything before the final '*'.
	prefix := pattern[:len(pattern)-1]
	return strings.HasPrefix(name, prefix)
}

// matchesAny reports whether name matches at least one pattern in patterns.
func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if matchPattern(p, name) {
			return true
		}
	}
	return false
}

// allows applies the include/exclude outcome rules to a measurement name.
func (f *MeasurementFilter) allows(name string) bool {
	if len(f.Include) == 0 && len(f.Exclude) == 0 {
		return true
	}
	if len(f.Include) > 0 && !matchesAny(f.Include, name) {
		return false
	}
	if len(f.Exclude) > 0 && matchesAny(f.Exclude, name) {
		return false
	}
	return true
}

// Compile-time check that MeasurementFilter implements Backend.
var _ Backend = (*MeasurementFilter)(nil)
