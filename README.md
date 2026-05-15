# go-monitor

A shared Go library for building monitoring daemons. Provides the common infrastructure — config, logging, backends, polling, signals, and shutdown — so you only write the collection logic.

## Features

- **One-function interface**: Provide a `CollectFunc`, the library handles everything else
- **Multiple backends**: InfluxDB 2.x, Elasticsearch (bulk API), Prometheus exporter, echo (debug/stdout)
- **Per-backend routing**: `MeasurementFilter` wraps any backend and forwards only metrics whose measurement matches configured include/exclude patterns
- **Metrics pipeline**: Batched delivery with configurable retry and health-aware backend dispatch — an unhealthy backend is re-probed after `RecoverInterval` (default 30s) instead of being skipped permanently
- **Signal handling**: SIGINT/SIGTERM for graceful shutdown, SIGHUP for config reload
- **TOML configuration**: Structured config with validation and sensible defaults
- **Structured logging**: slog-based with runtime-updatable log levels

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/danweinerdev/go-monitor"
)

func main() {
    m, err := monitor.New("mymonitor", collectFunc,
        monitor.WithConfigFile("config.toml"),
    )
    if err != nil {
        log.Fatal(err)
    }
    m.Run(context.Background())
}

func collectFunc(ctx context.Context) ([]*monitor.Metric, error) {
    return []*monitor.Metric{
        monitor.NewMetric("temperature").
            WithTag("location", "office").
            WithField("celsius", 22.5),
    }, nil
}
```

## Installation

```bash
go get github.com/danweinerdev/go-monitor
```

Sub-packages are imported separately so unused backends don't add dependencies:

```bash
go get github.com/danweinerdev/go-monitor/influxdb
go get github.com/danweinerdev/go-monitor/elasticsearch
go get github.com/danweinerdev/go-monitor/promexporter
```

## Configuration

TOML configuration with sensible defaults:

```toml
[global]
poll_interval = "10s"
log_level = "info"
batch_size = 10
retry_attempts = 3
retry_delay = "1s"

[influxdb]
enabled = true
url = "http://localhost:8086"
token = "your-token"
org = "myorg"
bucket = "mybucket"

[prometheus]
enabled = true
port = 9090
path = "/metrics"
```

### Defaults

| Setting | Default |
|---------|---------|
| `global.poll_interval` | `10s` |
| `global.log_level` | `info` |
| `global.batch_size` | `10` |
| `global.retry_attempts` | `3` |
| `global.retry_delay` | `1s` |
| `prometheus.port` | `9090` |
| `prometheus.path` | `/metrics` |

## Usage

### Using Backends

```go
import (
    "github.com/danweinerdev/go-monitor"
    "github.com/danweinerdev/go-monitor/influxdb"
    "github.com/danweinerdev/go-monitor/promexporter"
)

cfg, _ := monitor.LoadConfig("config.toml")

m, err := monitor.New("mymonitor", collectFunc,
    monitor.WithConfig(cfg),
    monitor.WithBackend(influxdb.New(cfg.InfluxDB, nil)),
    monitor.WithBackend(promexporter.New(cfg.Prometheus, nil)),
)
```

### Echo Mode (Debug)

```go
m, err := monitor.New("mymonitor", collectFunc,
    monitor.WithEcho(true),
)
```

### Run Once (Single Collection)

```go
m, err := monitor.New("mymonitor", collectFunc,
    monitor.WithConfigFile("config.toml"),
    monitor.WithRunOnce(true),
    monitor.WithEcho(true),
)
```

### Custom Reload Logic

```go
m, err := monitor.New("mymonitor", collectFunc,
    monitor.WithConfigFile("config.toml"),
    monitor.WithReloadFunc(func(path string) (*monitor.Config, error) {
        cfg, err := monitor.LoadConfig(path)
        if err != nil {
            return nil, err
        }
        // custom reload logic here
        return cfg, nil
    }),
)
```

### Per-Backend Routing

`MeasurementFilter` wraps any `Backend` and only forwards metrics whose
measurement name matches its patterns. Patterns are exact strings or a single
trailing `*` prefix wildcard (e.g. `"docsis_*"`); `Include` is an allowlist,
`Exclude` is a denylist applied after it. Use it to send different measurements
to different backends:

```go
import (
    "github.com/danweinerdev/go-monitor"
    "github.com/danweinerdev/go-monitor/elasticsearch"
    "github.com/danweinerdev/go-monitor/influxdb"
)

influx := &monitor.MeasurementFilter{
    Inner:   influxdb.New(influxCfg, nil),
    Exclude: []string{"events"},          // metrics → InfluxDB, but not "events"
}
es := &monitor.MeasurementFilter{
    Inner:   elasticsearch.New(esCfg, nil),
    Include: []string{"events"},          // only "events" → Elasticsearch
}

m, err := monitor.New("mymonitor", collectFunc,
    monitor.WithConfig(cfg),
    monitor.WithBackend(influx),
    monitor.WithBackend(es),
)
```

The Elasticsearch backend writes via the bulk API and uses a configurable tag
(`IDFromTag`, default `doc_id`) as the document `_id` for idempotent
re-delivery; that tag is excluded from the document body.

### Backend Health & Recovery

The pipeline skips a backend whose `Healthy()` is false, but only until
`RecoverInterval` (default 30s) has elapsed, then it sends one probe flush.
Backends mark themselves healthy at the top of `Write` and unhealthy on
failure, so a transient outage self-heals on the next probe instead of
stranding the backend until restart.

### Poll Statistics

```go
m.Run(ctx)
stats := m.Stats()
fmt.Printf("Polls: %d, Metrics: %d\n", stats.TotalPolls, stats.TotalMetrics)
```

## Options

| Option | Description |
|--------|-------------|
| `WithConfigFile(path)` | Load config from a TOML file |
| `WithConfig(cfg)` | Provide config directly |
| `WithEcho(true)` | Output metrics to stdout |
| `WithRunOnce(true)` | Collect once and exit |
| `WithLogger(logger)` | Use a custom slog.Logger |
| `WithBackend(b)` | Add a custom backend |
| `WithReloadFunc(fn)` | Custom config reload on SIGHUP |

## Package Structure

```
go-monitor/
├── metric.go             # Metric data model + builder + line protocol
├── backend.go            # Backend interface + Echo + MultiBackend
├── filter.go             # MeasurementFilter (per-backend measurement routing)
├── pipeline.go           # Batching pipeline with retry + recover-interval probe
├── signal.go             # Signal handling (SIGINT/SIGTERM/SIGHUP)
├── config.go             # Config types + TOML loading + defaults
├── validation.go         # Validation framework
├── logging.go            # slog setup helpers
├── stats.go              # Poll statistics tracking
├── options.go            # Functional options for Monitor
├── monitor.go            # Core runtime (poll loop, signals, shutdown)
├── influxdb/
│   └── influxdb.go       # InfluxDB v2 backend
├── elasticsearch/
│   └── elasticsearch.go  # Elasticsearch bulk-API backend
└── promexporter/
    └── promexporter.go   # Prometheus exporter backend
```

InfluxDB, Elasticsearch, and Prometheus are isolated sub-packages so monitors that don't use them avoid pulling in those dependencies.

## Dependencies

- [BurntSushi/toml](https://github.com/BurntSushi/toml) — TOML configuration
- [InfluxDB Client](https://github.com/influxdata/influxdb-client-go) — InfluxDB 2.x (sub-package only)
- [go-elasticsearch](https://github.com/elastic/go-elasticsearch) — Elasticsearch v8 (sub-package only)
- [Prometheus Client](https://github.com/prometheus/client_golang) — Prometheus metrics (sub-package only)
- `log/slog` — Structured logging (standard library)

## Requirements

- Go 1.25.5+

## License

MIT License. See [LICENSE](LICENSE) file for details.
