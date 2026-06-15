# Melody integrations

Integrations are optional modules that connect Melody to third-party systems (databases, Redis, message
brokers, object storage, observability). Each integration is a **separate Go module** with its own version
line, so you only pull in what you use.

**v3 is the actively maintained line.** New integration features land on v3; v1/v2 bindings receive fixes
only (see the repository [`CONTRIBUTING.md`](../CONTRIBUTING.md) and [`SECURITY.md`](../SECURITY.md)).

## Available integrations

| Integration | What it provides | Version lines | Docs |
| --- | --- | --- | --- |
| **amqp** | RabbitMQ transport for the message bus (publisher confirms, auto-reconnect, dead-lettering). | v3 | [README](./amqp/v3/README.md) |
| **awss3** | S3-compatible object-storage backend for the `storage` package. | v3 | [README](./awss3/v3/README.md) |
| **bunorm** | [Bun](https://bun.uptrace.dev/) ORM database access, with column encryption and audit helpers. | v1 / v2 / v3 | [README](./bunorm/README.md) |
| **bunorm/migrate** | Database migration CLI commands. | v1 / v2 / v3 | [README](./bunorm/migrate/README.md) |
| **bunorm/mysql** | MySQL driver wiring and distributed-lock backend. | v1 / v2 / v3 | [README](./bunorm/mysql/README.md) |
| **bunorm/pgsql** | PostgreSQL driver wiring and distributed-lock backend. | v1 / v2 / v3 | [README](./bunorm/pgsql/README.md) |
| **cron** | Crontab generation from registered schedules (`melody:cron:generate`). | v1 / v2 / v3 | [README](./cron/README.md) |
| **opentelemetry** | HTTP metrics/observability (Prometheus exposition) wiring. | v3 | [README](./opentelemetry/v3/README.md) |
| **rueidis** | Redis-backed cache, distributed lock, token store, and SSE backplane. | v1 / v2 / v3 | [README](./rueidis/README.md) |
| **websocket** | WebSocket support (connection hub, bound to the server-sent-event hub). | v3 | [README](./websocket/v3/README.md) |

## Usage

Each integration ships a `module.go` with `Register*` helpers that follow the same plug-and-play pattern, so
wiring one looks the same as wiring any other. See each integration's README for import paths and a minimal
example, and [`../v3/.example`](../v3/.example) for all of them wired together in one runnable application.
