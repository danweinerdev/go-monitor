package elasticsearch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	monitor "github.com/danweinerdev/go-monitor"
)

// captureHandler is a slog.Handler that records emitted records for assertions.
type captureHandler struct {
	records *[]slog.Record
}

func newCaptureLogger() (*slog.Logger, *[]slog.Record) {
	recs := &[]slog.Record{}
	h := &captureHandler{records: recs}
	return slog.New(h), recs
}

// esProductHeader marks a response as coming from genuine Elasticsearch.
// The official client refuses to talk to a server that does not send this
// header, so test servers must emulate a real cluster.
func esProductHeader(w http.ResponseWriter) {
	w.Header().Set("X-Elastic-Product", "Elasticsearch")
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func recordContains(recs []slog.Record, level slog.Level, substr string) bool {
	for _, r := range recs {
		if r.Level != level {
			continue
		}
		var found bool
		if strings.Contains(r.Message, substr) {
			found = true
		}
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), substr) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// parseBulk splits an NDJSON bulk body into action/doc line pairs.
func parseBulk(t *testing.T, body []byte) (actions, docs []map[string]any) {
	t.Helper()
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []map[string]any
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bulk line is not valid JSON: %q: %v", line, err)
		}
		lines = append(lines, m)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning bulk body: %v", err)
	}
	if len(lines)%2 != 0 {
		t.Fatalf("expected even number of bulk lines, got %d", len(lines))
	}
	for i := 0; i < len(lines); i += 2 {
		actions = append(actions, lines[i])
		docs = append(docs, lines[i+1])
	}
	return actions, docs
}

func testMetric() *monitor.Metric {
	m := monitor.NewMetric("mb8611_event")
	m.Tags = map[string]string{"doc_id": "abc-123", "host": "router"}
	m.Fields = map[string]any{"value": 42, "ok": true}
	m.Timestamp = time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC)
	return m
}

func newTestBackend(t *testing.T, url string, cfg Config) (*Backend, *[]slog.Record) {
	t.Helper()
	cfg.URL = url
	cfg.InsecureTLS = true
	if cfg.Index == "" {
		cfg.Index = "mb8611-events"
	}
	logger, recs := newCaptureLogger()
	b := New(cfg, logger)
	if err := b.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	return b, recs
}

func TestNewBackend(t *testing.T) {
	b := New(Config{URL: "https://localhost:9200", Index: "x"}, nil)
	if b.Name() != "elasticsearch" {
		t.Errorf("Name() = %q, want %q", b.Name(), "elasticsearch")
	}
	if b.Healthy() {
		t.Error("Backend should not be healthy before Initialize()")
	}
}

func TestWriteEmptyBatch(t *testing.T) {
	b := New(Config{}, nil)
	if err := b.Write(context.Background(), nil); err != nil {
		t.Errorf("Write() with empty batch should not error, got %v", err)
	}
}

func TestCloseNilClient(t *testing.T) {
	b := New(Config{}, nil)
	if err := b.Close(); err != nil {
		t.Errorf("Close() with nil client should not error, got %v", err)
	}
}

func TestWriteHappyPath(t *testing.T) {
	var gotBody []byte
	var gotPath string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esProductHeader(w)
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			// Ping during Initialize.
			w.WriteHeader(http.StatusOK)
			return
		}
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":false,"items":[{"index":{"_id":"abc-123","status":201}}]}`))
	}))
	defer srv.Close()

	b, _ := newTestBackend(t, srv.URL, Config{Index: "mb8611-events"})

	if err := b.Write(context.Background(), []*monitor.Metric{testMetric()}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !b.Healthy() {
		t.Error("backend should be healthy after successful write")
	}

	if !strings.Contains(gotPath, "_bulk") {
		t.Errorf("expected bulk endpoint, got path %q", gotPath)
	}

	actions, docs := parseBulk(t, gotBody)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action/doc pair, got %d", len(actions))
	}

	idx, ok := actions[0]["index"].(map[string]any)
	if !ok {
		t.Fatalf("action line missing index object: %v", actions[0])
	}
	if idx["_index"] != "mb8611-events" {
		t.Errorf("_index = %v, want mb8611-events", idx["_index"])
	}
	if idx["_id"] != "abc-123" {
		t.Errorf("_id = %v, want abc-123", idx["_id"])
	}

	doc := docs[0]
	if _, exists := doc["doc_id"]; exists {
		t.Errorf("document MUST NOT contain the ID tag key, got %v", doc)
	}
	if doc["host"] != "router" {
		t.Errorf("expected non-ID tag host=router preserved, got %v", doc["host"])
	}
	if _, exists := doc["@timestamp"]; !exists {
		t.Errorf("document missing @timestamp, got %v", doc)
	}
	if doc["measurement"] != "mb8611_event" {
		t.Errorf("document measurement = %v, want mb8611_event", doc["measurement"])
	}
	if doc["value"] == nil || doc["ok"] != true {
		t.Errorf("document missing fields, got %v", doc)
	}
}

func TestWritePartialFailure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esProductHeader(w)
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":true,"items":[
			{"index":{"_id":"ok-1","status":201}},
			{"index":{"_id":"bad-2","status":400,"error":{"type":"mapper_parsing_exception","reason":"failed to parse field"}}}
		]}`))
	}))
	defer srv.Close()

	b, recs := newTestBackend(t, srv.URL, Config{})

	m1 := testMetric()
	m1.Tags["doc_id"] = "ok-1"
	m2 := testMetric()
	m2.Tags["doc_id"] = "bad-2"

	if err := b.Write(context.Background(), []*monitor.Metric{m1, m2}); err != nil {
		t.Fatalf("Write() with partial failure should return nil, got %v", err)
	}
	if !b.Healthy() {
		t.Error("backend should stay healthy on partial failure")
	}
	if !recordContains(*recs, slog.LevelError, "bad-2") {
		t.Error("expected the failing _id 'bad-2' logged at error level")
	}
}

func TestWriteAllFailure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esProductHeader(w)
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":true,"items":[
			{"index":{"_id":"bad-1","status":400,"error":{"type":"mapper_parsing_exception","reason":"boom"}}},
			{"index":{"_id":"bad-2","status":400,"error":{"type":"mapper_parsing_exception","reason":"boom"}}}
		]}`))
	}))
	defer srv.Close()

	b, recs := newTestBackend(t, srv.URL, Config{})

	m1 := testMetric()
	m1.Tags["doc_id"] = "bad-1"
	m2 := testMetric()
	m2.Tags["doc_id"] = "bad-2"

	if err := b.Write(context.Background(), []*monitor.Metric{m1, m2}); err == nil {
		t.Fatal("Write() should return an error when all items fail")
	}
	if b.Healthy() {
		t.Error("backend should be unhealthy when all items fail")
	}
	if !recordContains(*recs, slog.LevelError, "bad-1") {
		t.Error("expected failing _id 'bad-1' logged at error level")
	}
}

func TestWriteNetworkError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esProductHeader(w)
		w.WriteHeader(http.StatusOK)
	}))
	b, _ := newTestBackend(t, srv.URL, Config{})
	// Kill the server so the subsequent bulk POST fails at transport level.
	srv.Close()

	if err := b.Write(context.Background(), []*monitor.Metric{testMetric()}); err == nil {
		t.Fatal("Write() should return an error on network failure")
	}
	if b.Healthy() {
		t.Error("backend should be unhealthy after network failure")
	}
}

func TestWriteDoesNotMutateMetric(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esProductHeader(w)
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":false,"items":[{"index":{"_id":"abc-123","status":201}}]}`))
	}))
	defer srv.Close()

	b, _ := newTestBackend(t, srv.URL, Config{})

	m := testMetric()
	clone := m.Clone()
	batch := []*monitor.Metric{m}

	if err := b.Write(context.Background(), batch); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if !reflect.DeepEqual(m.Tags, clone.Tags) {
		t.Errorf("input Metric.Tags mutated: got %v, want %v", m.Tags, clone.Tags)
	}
	if !reflect.DeepEqual(m.Fields, clone.Fields) {
		t.Errorf("input Metric.Fields mutated: got %v, want %v", m.Fields, clone.Fields)
	}
	if _, ok := m.Tags["doc_id"]; !ok {
		t.Error("ID tag was stripped from the input Metric (must only affect the emitted doc)")
	}
	if m.Measurement != clone.Measurement || !m.Timestamp.Equal(clone.Timestamp) {
		t.Error("input Metric scalar fields mutated")
	}
}

func TestWriteCustomIDFromTag(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		esProductHeader(w)
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":false,"items":[{"index":{"_id":"evt-9","status":201}}]}`))
	}))
	defer srv.Close()

	b, _ := newTestBackend(t, srv.URL, Config{IDFromTag: "event_id"})

	m := monitor.NewMetric("mb8611_event")
	m.Tags = map[string]string{"event_id": "evt-9", "doc_id": "should-stay"}
	m.Fields = map[string]any{"n": 1}
	m.Timestamp = time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	if err := b.Write(context.Background(), []*monitor.Metric{m}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	actions, docs := parseBulk(t, gotBody)
	idx := actions[0]["index"].(map[string]any)
	if idx["_id"] != "evt-9" {
		t.Errorf("_id = %v, want evt-9 (from custom IDFromTag)", idx["_id"])
	}
	if _, exists := docs[0]["event_id"]; exists {
		t.Errorf("document must not contain the custom ID tag 'event_id', got %v", docs[0])
	}
	if docs[0]["doc_id"] != "should-stay" {
		t.Errorf("non-ID tag 'doc_id' should remain in the doc when IDFromTag is custom, got %v", docs[0])
	}
}
