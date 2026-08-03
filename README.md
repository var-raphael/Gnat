# Gnat

Small, quiet, everywhere. Self-hosted analytics without the bloat.

Gnat is a single lightweight Go binary for self-hosted, privacy-first web analytics. Download it, write a small config file, run it. No Docker, no multi-service stack, no per-language SDKs. Any backend just POSTs JSON.

## Why Gnat

Most self-hosted analytics tools force a tradeoff. GoatCounter is simple to run but light on features. Plausible and PostHog are feature-rich but need a stack of services to operate. Gnat aims to combine the deployment simplicity of a single binary with real analytics depth, plus an AI-native layer that none of the others offer yet.

- Single Go binary, no CGO required
- SQLite, Postgres, or MySQL, your choice
- Raw HTTP ingestion, no SDKs needed for any backend language
- Funnels, cohorts, retention curves, and auto-discovered path analysis
- Built-in MCP server so AI agents can query your analytics directly
- Optional AI-generated summaries of your stats, using your own API key
- CSV, JSON, and raw SQL export, it is your data

## Status

Early development. Not ready for production use yet.

## Quickstart

```bash
go build ./...
go run ./cmd/gnat -config ./gnat.yaml
```

Minimal config file:

```yaml
server:
  bind_port: 8080
  public_url: "http://localhost:8080"

database:
  driver: sqlite
  path: "./analytics.db"

api_key: "generate-a-random-secret-here"

retention:
  raw_events_days: 0

updates:
  check_for_updates: true
```

## Architecture

Everything runs in one process:

- HTTP server and event ingestion
- Storage via GORM, swappable between SQLite, Postgres, and MySQL
- Query API that powers both the embedded dashboard and any custom UI
- Embedded dashboard, served as static files via Go's embed package
- Background jobs for path precomputation, retention cleanup, and AI summaries
- MCP server for AI agent access

## License

Source-available, Elastic License 2.0 style. See `LICENSE`.

