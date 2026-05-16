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

The example demonstrates Melody's [`integrations/cron`](../integrations/cron/) package. Commands stay plain Melody CLI commands — there is no `cron.Metadata` interface to implement. Schedules are declared separately in [`config/cli.go`](./config/cli.go) through a `cron.Configuration` registry:

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

Cron defaults (user, heartbeat path, logs directory, destination file, template) come from the parameter system in [`config/parameter.go`](./config/parameter.go), with user and heartbeat path sourced from `APP_CRON_USER` and `APP_CRON_HEARTBEAT_PATH` env vars in [`.env`](./.env). [`config/cron.go`](./config/cron.go) reads `app.cron.product_user` (backed by `APP_CRON_PRODUCT_USER`) at registration time and applies it as the per-command user on the `product:list` schedule, demonstrating how the parameter cascade feeds custom values into `cron.Configuration` entries.

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

The example is a standalone Go module (`.example/go.mod`) that depends on Melody and the cron integration. From the repository root:

```bash
cd .example
go run .
```

For a fully self-contained binary that embeds `.env` files and `public/` assets into the executable:

```bash
cd .example
go run -tags "melody_env_embedded melody_static_embedded" .
```

Tags can be combined independently — use only `melody_env_embedded` to embed env files, only `melody_static_embedded` to embed static assets, or both.

Once started, open the application in your browser:

- http://localhost:8080

### CLI mode

The example also wires CLI commands. List them:

```bash
cd .example
go run . -h
```

Among the commands you will find `melody:cron:generate` from the cron integration. To generate a crontab fragment from the `cron.Configuration` registered in [`config/cli.go`](./config/cli.go):

```bash
cd .example
go run . melody:cron:generate --out ./generated_conf/cron/crontab
```

The example registers two scheduled commands in [`config/cli.go`](./config/cli.go) (`product:list` every 6 hours, `app:info` daily at noon) plus a heartbeat configured via `APP_CRON_HEARTBEAT_PATH` in [`.env`](./.env), so the generated crontab is not empty.

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
