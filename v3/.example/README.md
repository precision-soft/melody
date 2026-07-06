# Melody Example Application (`.example`)

The `.example` directory contains a small **product catalog** application built as a **feature showcase** for the Melody framework.

It is **not** a full production product. Its purpose is to demonstrate how Melody is intended to be used in userland, with realistic wiring and clear architectural boundaries: routing, HTTP handlers, dependency injection, structured logging, sessions and authentication, security access control, caching, events, and CLI commands.

---

## What it represents

Conceptually, the example models a minimal admin-style catalog application:

- Product listing and detail pages
- Login / logout flow based on sessions
- A simple role system (`ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`)
- HTML pages backed by JSON endpoints (consumed via jQuery)
- CLI commands that demonstrate Melody’s CLI conventions and container/runtime usage

---

## Demo credentials

For convenience, the example ships with a few predefined users:

- `user` / `user` — `ROLE_USER`
- `editor` / `editor` — `ROLE_USER`, `ROLE_EDITOR`
- `admin` / `admin` — `ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`

---

## Structure overview

The example lives entirely under the [`./.example/`](./) directory and follows a **flat layout**: each concern lives in its own top-level package, with no `domain/` / `infra/` umbrella layers. All paths below are **relative to `.example/`**.

```
.example/
├── cache/            # cache serializer for the example container
├── cli/              # CLI commands (app:info, product:list)
├── config/           # application wiring; one file per module hook
├── entity/           # domain entities (Category, Currency, Product, User)
├── event/            # domain event types
├── handler/          # HTTP handlers (pages + JSON APIs), with category/, currency/, product/, user/ subpackages
├── page/             # HTML page templates
├── presenter/        # HTTP error / response presenters
├── repository/       # repository interfaces + in-memory implementations
├── route/            # named route constants and patterns
├── security/         # session auth wiring (login/logout handlers, entry point, token resolver, password hasher)
├── service/          # application services (CategoryService, CurrencyService, ProductService, UserService)
├── subscriber/       # event subscribers
├── url/              # URL generation + route registry adapters
├── public/           # static assets (CSS / JS)
├── embedded_*.go     # build-tag–controlled embedding for env and static assets
├── main.go           # application entry point
├── go.mod / go.sum   # standalone module manifest
├── .env              # example env defaults
└── .gitignore
```

### [`config/`](./config/) — application wiring

The [`config/`](./config/) package keeps [`main.go`](./main.go) small by grouping all setup and integration logic in a single place, with each module hook in its own file:

- [`configure.go`](./config/configure.go) — entry point invoked by `main.go`: registers services, the example module, and the HTTP middleware
- [`module.go`](./config/module.go) — `Module` struct + `Name()` + `Description()` + interface assertions for the module hooks the example implements
- [`security.go`](./config/security.go) — `RegisterSecurity`: access-control rules, role hierarchy, decision manager, firewall
- [`http.go`](./config/http.go) — `RegisterHttpRoutes`: named-route registration for pages and JSON APIs
- [`cli.go`](./config/cli.go) — `RegisterCliCommands`: example CLI commands and the `melody:cron:generate` command wired through the cron `Configuration` registry
- [`event.go`](./config/event.go) — `RegisterEventSubscribers`: wires the example's domain event subscribers
- [`parameter.go`](./config/parameter.go) — `RegisterParameters`: registers `melody.cron.*` parameters from `APP_CRON_*` env vars plus the example's own `app.*` parameters
- [`service.go`](./config/service.go) — `registerServices`: container wiring for repositories, services, and the cache serializer
- [`middleware.go`](./config/middleware.go) — example-specific HTTP middleware (`NewTimingMiddleware`)

### Cron integration

The example demonstrates Melody's [`integrations/cron`](../../integrations/cron/v3/) package. Commands stay plain Melody CLI commands — there is no `cron.Metadata` interface to implement. Schedules are declared separately in [`config/cli.go`](./config/cli.go) through a `cron.Configuration` registry:

```go
cronConfiguration := cron.NewConfiguration().
    Schedule(cron.CommandName(cli.NewProductListCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "0", Hour: "*/6"},
    }).
    Schedule(cron.CommandName(cli.NewAppInfoCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "0", Hour: "12"},
    })
```

`cron.CommandName` is a generic helper that instantiates a constructor and returns the command name, so the schedule references commands by constructor instead of hardcoded strings.

Cron defaults (user, logs directory, destination file, template, heartbeat) come from the parameter system in [`config/parameter.go`](./config/parameter.go). The user is sourced from `APP_CRON_USER`, and the heartbeat is enabled via the `APP_CRON_HEARTBEAT_AUTO_ENABLED` opt-in (which auto-derives `<logs-dir>/heartbeat.crontab` from `melody.cron.logs_dir`) — both env vars live in [`.env`](./.env). [`config/cron.go`](./config/cron.go) reads `app.cron.product_user` (backed by `APP_CRON_PRODUCT_USER`) at registration time and applies it as the per-command user on the `product:list` schedule, demonstrating how the parameter cascade feeds custom values into `cron.Configuration` entries.

### [`main.go`](./main.go) (why it stays small)

`main.go` intentionally contains minimal logic.

It only:

- constructs the Melody application using:
    - `embeddedEnvFiles` (from `embedded_env_*`)
    - `embeddedPublicFiles` (from `embedded_static_*`)
- calls `config.Configure(app)`
- runs the application

All wiring and integration logic lives outside `main.go`.

---

## Running locally

The example is a standalone Go module (`v3/.example/go.mod`) that depends on Melody and its platform integrations (see [Platform integrations](#platform-integrations-optional-env-gated) below). From the repository root:

```bash
cd v3/.example
go run .
```

For a fully self-contained binary that embeds `.env` files and `public/` assets into the executable:

```bash
cd v3/.example
go run -tags "melody_env_embedded melody_static_embedded" .
```

Tags can be combined independently — use only `melody_env_embedded` to embed env files, only `melody_static_embedded` to embed static assets, or both.

Once started, open the application in your browser:

- http://localhost:8080

> The committed [`.env`](./.env) points the integration endpoints at the dev compose service names (`redis:6379`, `mysql`, …), so the fully-wired experience is [`./dc up:all --build`](#running-fully-against-containers), which runs this same app inside the dev container where those names resolve. A bare host `go run .` needs those services reachable (override the endpoints to the mapped host ports, [see below](#running-the-binary-directly-against-mapped-ports)) — or remove their lines from `.env` to boot with the in-process fallbacks and zero infrastructure.

### CLI mode

The example also wires CLI commands. List them:

```bash
cd v3/.example
go run . -h
```

Among the commands you will find `melody:cron:generate` from the cron integration. To generate a crontab fragment from the `cron.Configuration` registered in [`config/cli.go`](./config/cli.go):

```bash
cd v3/.example
go run . melody:cron:generate --out ./generated_conf/cron/crontab
```

The example registers two scheduled commands in [`config/cli.go`](./config/cli.go) (`product:list` every 6 hours, `app:info` daily at noon) plus a heartbeat enabled via `APP_CRON_HEARTBEAT_AUTO_ENABLED=true` in [`.env`](./.env) (the path is auto-derived from `melody.cron.logs_dir`), so the generated crontab is not empty.

---

## Platform integrations (optional, env-gated)

The example wires **every v3 platform integration**. Each backend that needs external infrastructure is **gated on an environment variable**: when the variable is unset the application boots with an in-process fallback (remove the integration lines from [`.env`](./.env) and the example runs with zero infrastructure), and when it is set the matching integration is activated and resolved through the same core service constant, so the rest of the app is unchanged. The committed `.env` sets these variables to the dev compose service endpoints so `./dc up:all` wires everything out of the box.

| Integration                                                                                                                                      | Activated by                                 | Falls back to            | Demo endpoint                                    |
|--------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------|--------------------------|--------------------------------------------------|
| [`opentelemetry`](../../integrations/opentelemetry/v3/) — Prometheus metrics middleware                                                          | always on                                    | —                        | `GET /metrics`                                   |
| [`websocket`](../../integrations/websocket/v3/) — WebSocket bound to the SSE hub                                                                 | always on                                    | —                        | `GET /ws` (and `GET /events/stream` for SSE)     |
| `encrypt` ([`bunorm/v3/encrypt`](../../integrations/bunorm/v3/encrypt/)) — AES-256-GCM cipher                                                    | always on                                    | —                        | `GET /encrypt/demo`                              |
| [`amqp`](../../integrations/amqp/v3/) — durable message-bus transport                                                                            | `AMQP_DSN`                                   | in-memory transport      | `POST /messagebus/demo`                          |
| [`awss3`](../../integrations/awss3/v3/) — S3 object storage (`storage.ServiceStorage`)                                                           | `S3_ENDPOINT`                                | local filesystem storage | `GET /platform/demo`                             |
| [`rueidis`](../../integrations/rueidis/v3/) — Redis cache backend, distributed lock (`lock.ServiceLocker`), revocable token store, SSE backplane | `REDIS_ADDRESS`                              | in-memory cache/lock     | `GET /cache/demo`, `GET /redis/token/demo`       |
| [`bunorm/mysql`](../../integrations/bunorm/mysql/v3/) — MySQL `GET_LOCK` distributed lock                                                        | `MYSQL_HOST` (when `REDIS_ADDRESS` is unset) | in-memory lock           | `GET /platform/demo`                             |
| [`bunorm`](../../integrations/bunorm/v3/) — bun ORM `*bun.DB`, transparent column encryption, field-level audit trail                            | `MYSQL_HOST`                                 | —                        | `GET /database/demo`, `GET /database/audit/demo` |

The lock service follows a single priority: Redis if configured, otherwise MySQL, otherwise in-memory. `GET /database/demo` writes an `encrypt.EncryptedString` column and reads it back — the response shows the decrypted value next to the raw ciphertext stored in MySQL (`<ENC>…`), demonstrating transparent encryption-at-rest.

### Running fully against containers

The dev [`docker-compose.yml`](../../.dev/docker/docker-compose.yml) provides the whole backing stack — RabbitMQ, Redis, MySQL, PostgreSQL, LocalStack (S3), Mailpit (SMTP), Prometheus, and an OpenTelemetry collector. The one-command way to run the entire showcase is the [`./dc`](../../dc) wrapper:

```bash
# from the repository root — build the dev image and start the example + every backend
./dc up:all --build
```

This brings up the backing services **and** the example itself: the `dev` container builds the frontend bundle and runs the app under a hot-reload + restart supervisor. The example reads its integration endpoints from [`.env`](./.env), which points at the compose service names (`REDIS_ADDRESS=redis:6379`, `MYSQL_HOST=mysql`, `AMQP_DSN=amqp://…@rabbitmq:5672/`, `S3_ENDPOINT=localstack:4566`, `SMTP_ADDRESS=mailpit:1025`, `OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317`), so no manual configuration is needed. Open it on the mapped host port:

- http://localhost:8180 (the `DEV_HTTP_HOST_PORT`, `8180` by default)

Notes:

- **`--build` rebuilds the dev image** — use it after changing dependencies or the container [`entrypoint.sh`](../../.dev/docker/entrypoint.sh). For day-to-day Go/HTML/asset edits `./dc up:all` (without `--build`) is enough; reflex hot-reloads them.
- **Always `up:all`, not plain `up`.** The backends live on the compose `all` profile, so `./dc up` / `./dc up:minimal` start only the dev container and load balancer — the example would then have no Redis/MySQL to reach. Use `./dc up:all` whenever you want the integration demos.
- **Cold-start is self-healing.** If a backend is not ready yet — or you start it afterwards — the MySQL/Redis providers retry the initial connection with backoff and the entrypoint supervisor restarts the process, so the app comes up on its own without a manual restart.

The demo endpoints listed above then work against the mapped host port, e.g. `curl localhost:8180/database/demo`, `curl localhost:8180/cache/demo`.

#### Running the binary directly against mapped ports

To run `go run .` on the host (outside the dev container) instead, point the integrations at the **mapped host ports** — an OS environment variable overrides the matching `.env` value:

```bash
cd v3/.example
AMQP_DSN="amqp://guest:guest@localhost:5673/" \
REDIS_ADDRESS="localhost:6380" \
S3_ENDPOINT="localhost:4566" S3_ACCESS_KEY="test" S3_SECRET_KEY="test" S3_BUCKET="melody-example" \
MYSQL_HOST="localhost" MYSQL_PORT="3307" MYSQL_DATABASE="melody_example" MYSQL_USER="melody" MYSQL_PASSWORD="melody" \
go run .
```

---

## API response envelope

Most JSON endpoints return a small, consistent response envelope:

- `status`
- optional `data`
- optional `error`

This keeps frontend code predictable and minimizes ad-hoc handling.

---

## Build modes: embedded vs filesystem

The example supports **two independent resource families**, each of which can be used either from the filesystem or embedded into the binary:

1. Environment configuration (`.env`-style files)
2. Static assets (`public/`)

They are controlled independently via build tags.

---

## 1) Environment configuration (`.env`)

**Relevant files (paths relative to `.example/`):**

- `embedded_env_local.go`
- `embedded_env_embedded.go`

**Build tag:**

- `melody_env_embedded`

### Behavior

- **Without** `melody_env_embedded`  
  Environment configuration is read from filesystem `.env` files.  
  For local development, place `.env` next to the binary or in the working directory.

- **With** `melody_env_embedded`  
  Environment configuration is embedded into the binary at build time.  
  The resulting binary can start without any external `.env` file.

---

## 2) Static assets (`public/`)

**Relevant files (paths relative to `.example/`):**

- `embedded_static_local.go`
- `embedded_static_embedded.go`

**Build tag:**

- `melody_static_embedded`

### Behavior

- **Without** `melody_static_embedded`  
  Static assets are served from the filesystem `public/` directory.

- **With** `melody_static_embedded`  
  Static assets are embedded into the binary.  
  No `public/` directory is required at runtime.

---

## Production packaging matrix

Depending on how you build the binary, you must ship different artifacts.

### A) Fully embedded “black-box” binary (recommended for demos)

Build:

```bash
go build -tags "melody_env_embedded melody_static_embedded" -o example-app .
```

Ship:

- `example-app` binary

Required at runtime:

- nothing else

---

### B) External configuration, embedded static assets

Build:

```bash
go build -tags "melody_static_embedded" -o example-app .
```

Ship:

- `example-app` binary
- `.env` file(s)

Required at runtime:

- `.env` file(s)

Not required:

- [`public/`](./public/) directory

---

### C) Embedded configuration, filesystem static assets

Build:

```bash
go build -tags "melody_env_embedded" -o example-app .
```

Ship:

- `example-app` binary
- [`public/`](./public/) directory

Required at runtime:

- [`public/`](./public/)

Not required:

- `.env`

---

### D) Filesystem configuration + filesystem static assets

Build:

```bash
go build -o example-app .
```

Ship:

- `example-app` binary
- `.env` file(s)
- [`public/`](./public/) directory

Required at runtime:

- `.env`
- [`public/`](./public/)

---

## Notes

- This example is intentionally compact and optimized for readability.
- Treat it as a **reference implementation** for Melody wiring patterns, not as a stable API contract.
- The framework APIs demonstrated here are authoritative; the example itself may evolve freely.
