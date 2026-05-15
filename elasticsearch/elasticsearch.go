// Package elasticsearch provides a monitor.Backend that writes metrics to
// Elasticsearch via the Bulk API using the official go-elasticsearch client.
package elasticsearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	monitor "github.com/danweinerdev/go-monitor"
	"github.com/elastic/go-elasticsearch/v8"
)

// defaultIDTag is the tag whose value becomes the Elasticsearch document _id
// when Config.IDFromTag is left empty.
const defaultIDTag = "doc_id"

// Config contains Elasticsearch connection and indexing settings.
type Config struct {
	Enabled     bool
	URL         string // e.g. "https://es.lan:9200"
	Index       string // e.g. "mb8611-events"
	Username    string // basic auth (optional)
	Password    string
	APIKey      string // alternative to basic auth; takes precedence if set
	InsecureTLS bool   // default false
	IDFromTag   string // tag whose value becomes the ES _id; default "doc_id" when empty
}

// Backend implements monitor.Backend for Elasticsearch.
type Backend struct {
	cfg    Config
	client *elasticsearch.Client
	logger *slog.Logger

	mu      sync.RWMutex
	healthy bool
}

// New creates a new Elasticsearch backend.
func New(cfg Config, logger *slog.Logger) *Backend {
	if logger == nil {
		logger = slog.Default()
	}
	return &Backend{
		cfg:    cfg,
		logger: logger,
	}
}

func (b *Backend) Name() string {
	return "elasticsearch"
}

// idTag returns the configured ID tag name, falling back to the default.
func (b *Backend) idTag() string {
	if b.cfg.IDFromTag != "" {
		return b.cfg.IDFromTag
	}
	return defaultIDTag
}

func (b *Backend) Initialize(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.logger.Info("connecting to Elasticsearch", "url", b.cfg.URL, "index", b.cfg.Index)

	esCfg := elasticsearch.Config{
		Addresses: []string{b.cfg.URL},
		Username:  b.cfg.Username,
		Password:  b.cfg.Password,
		APIKey:    b.cfg.APIKey,
	}

	if b.cfg.InsecureTLS {
		esCfg.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in via InsecureTLS
		}
	}

	client, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	res, err := client.Ping(client.Ping.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to connect to Elasticsearch: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("Elasticsearch ping failed: %s", res.String())
	}

	b.client = client
	b.healthy = true

	b.logger.Info("connected to Elasticsearch", "index", b.cfg.Index)
	return nil
}

// bulkResponse models the relevant parts of an Elasticsearch _bulk response.
type bulkResponse struct {
	Errors bool `json:"errors"`
	Items  []map[string]struct {
		ID     string `json:"_id"`
		Status int    `json:"status"`
		Error  *struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	} `json:"items"`
}

func (b *Backend) Write(ctx context.Context, batch []*monitor.Metric) error {
	// Force healthy=true at entry: the pipeline gates on Healthy() before
	// calling Write, so a backend that stays false is never retried. This
	// lets the next scheduled flush re-attempt. If this attempt fails we set
	// healthy back to false at the end.
	b.mu.Lock()
	b.healthy = true
	b.mu.Unlock()

	if len(batch) == 0 {
		return nil
	}

	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()

	if client == nil {
		b.markUnhealthy()
		return fmt.Errorf("Elasticsearch not initialized")
	}

	idTag := b.idTag()

	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	for _, m := range batch {
		id := ""
		if m.Tags != nil {
			id = m.Tags[idTag]
		}

		action := map[string]any{
			"index": map[string]any{
				"_index": b.cfg.Index,
				"_id":    id,
			},
		}
		if err := enc.Encode(action); err != nil {
			b.markUnhealthy()
			return fmt.Errorf("failed to encode bulk action: %w", err)
		}

		// Deep-copy what we need; never mutate the shared *Metric. The
		// document must NOT contain the ID tag.
		doc := make(map[string]any, len(m.Tags)+len(m.Fields)+2)
		for k, v := range m.Tags {
			if k == idTag {
				continue
			}
			doc[k] = v
		}
		for k, v := range m.Fields {
			doc[k] = v
		}
		doc["measurement"] = m.Measurement
		doc["@timestamp"] = m.Timestamp.UTC().Format(time.RFC3339Nano)

		if err := enc.Encode(doc); err != nil {
			b.markUnhealthy()
			return fmt.Errorf("failed to encode bulk document: %w", err)
		}
	}

	res, err := client.Bulk(
		bytes.NewReader(body.Bytes()),
		client.Bulk.WithContext(ctx),
		client.Bulk.WithIndex(b.cfg.Index),
	)
	if err != nil {
		b.markUnhealthy()
		return fmt.Errorf("failed to write to Elasticsearch: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		b.markUnhealthy()
		return fmt.Errorf("Elasticsearch bulk request failed: %s", res.String())
	}

	var parsed bulkResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		b.markUnhealthy()
		return fmt.Errorf("failed to decode Elasticsearch bulk response: %w", err)
	}

	if !parsed.Errors {
		b.logger.Debug("wrote metrics to Elasticsearch", "count", len(batch))
		return nil
	}

	succeeded := 0
	failed := 0
	for _, item := range parsed.Items {
		for _, result := range item {
			if result.Error != nil {
				failed++
				reason := result.Error.Reason
				errType := result.Error.Type
				b.logger.Error("Elasticsearch rejected document",
					"_id", result.ID,
					"type", errType,
					"reason", reason,
				)
			} else {
				succeeded++
			}
		}
	}

	// At least one item succeeded: re-shipping the whole batch would
	// duplicate the successes, so item-level failures are logged, not
	// retried here.
	if succeeded > 0 {
		b.logger.Warn("partial Elasticsearch bulk failure",
			"succeeded", succeeded,
			"failed", failed,
		)
		return nil
	}

	// Zero items succeeded (all failed): catches index-template/mapping
	// disasters.
	b.markUnhealthy()
	return fmt.Errorf("Elasticsearch bulk write failed for all %d documents", failed)
}

func (b *Backend) markUnhealthy() {
	b.mu.Lock()
	b.healthy = false
	b.mu.Unlock()
}

func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.client = nil
	b.healthy = false

	b.logger.Info("Elasticsearch connection closed")
	return nil
}

func (b *Backend) Healthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthy
}

// Compile-time check.
var _ monitor.Backend = (*Backend)(nil)
