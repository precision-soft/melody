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

## Seeded credentials

For convenience, the example ships with a few predefined users:

- `user` / `user` — `ROLE_USER`
- `editor` / `editor` — `ROLE_USER`, `ROLE_EDITOR`
- `admin` / `admin` — `ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`

---

## Structure overview

The example lives entirely under the [`./.example/`](./) directory and follows a **flat layout**: each concern lives in its own top-level package, with no `domain/` / `infra/` umbrella layers. All paths below are **relative to `.example/`**.

```
.example/
├── assets/           # frontend bundle SOURCE (app.ts, melody-routes.ts) built into public/assets/app.js
├── cache/            # cache serializer for the example container
├── cli/              # CLI commands (app:info, product:list, catalog:journal, catalog:report:refresh)
├── config/           # application wiring; one file per module hook
├── entity/           # domain entities (Category, Currency, Product, User)
├── event/            # domain event types
├── handler/          # HTTP handlers (pages + JSON APIs), with category/, currency/, product/, user/ subpackages
├── page/             # HTML page templates
├── presenter/        # HTTP error / response presenters
├── repository/       # repository interfaces with an in-memory and a database-backed implementation each, plus the seed data and the helpers both share
├── route/            # named route constants and patterns
├── security/         # session auth wiring (login/logout handlers, entry point, access denied handler, token resolver, password hasher)
├── service/          # application services (Category, Currency, Product, User, plus the catalog journal and the catalog report)
├── subscriber/       # event subscribers
├── url/              # route registry adapters: the route manifest every page is given
├── public/           # static assets; app.css and index.html are committed, the icons and app.js are produced by the assets/ build
├── embedded_*.go     # build-tag–controlled embedding for env and static assets
├── main.go           # application entry point
├── go.mod / go.sum   # standalone module manifest
├── .env              # example env defaults
└── .gitignore
```

`var/` and `generated_conf/` are runtime output and are git-ignored, so they appear once the application has run rather than in a fresh clone.

### [`config/`](./config/) — application wiring

The [`config/`](./config/) package keeps [`main.go`](./main.go) small by grouping all setup and integration logic in a single place, with each module hook in its own file:

- [`configure.go`](./config/configure.go) — entry point invoked by `main.go`: registers the example module, whose hooks below contribute everything else
- [`module.go`](./config/module.go) — `Module` struct + `Name()` + `Description()` + interface assertions for the module hooks the example implements
- [`security.go`](./config/security.go) — `RegisterSecurity`: access-control rules, role hierarchy, decision manager, firewall
- [`http.go`](./config/http.go) — `RegisterHttpRoutes`: named-route registration for pages and JSON APIs
- [`cli.go`](./config/cli.go) — `RegisterCliCommands`: example CLI commands and the `melody:cron:generate` command wired through the cron `Configuration` registry
- [`event.go`](./config/event.go) — `RegisterEventSubscribers`: wires the example's domain event subscribers
- [`parameter.go`](./config/parameter.go) — `RegisterParameters`: registers `melody.cron.*` parameters from `APP_CRON_*` env vars plus the example's own `app.*` parameters
- [`service.go`](./config/service.go) — `registerServices`: container wiring for repositories, services, and the cache serializer
- [`middleware.go`](./config/middleware.go) — example-specific HTTP middleware (`NewTimingMiddleware`)
- [`cron.go`](./config/cron.go) — the `cron.Configuration` the generate command renders: which command runs on which schedule, and as which system user
- [`database.go`](./config/database.go) — the bun manager registry and the MySQL provider, built only when the configuration published a host; an unset host leaves the registry nil and every repository falls back to its in-memory twin
- [`redis.go`](./config/redis.go) — the rueidis client, the cache backend bound to `cache.ServiceCacheBackend`, and the rate limiter the write routes are put behind; an unset address leaves all three absent
- [`bootstrap_resolver.go`](./config/bootstrap_resolver.go) — reads a parameter before the container exists, which is what lets the two files above decide whether to wire an integration at all

### Cron integration

The example demonstrates Melody's [`integrations/cron`](../integrations/cron/) package. Commands stay plain Melody CLI commands — there is no `cron.Metadata` interface to implement. Schedules are declared separately in [`config/cron.go`](./config/cron.go) through a `cron.Configuration` registry:

```go
cronConfiguration := cron.NewConfiguration().
    Schedule(cron.CommandName(cli.NewCatalogReportRefreshCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "0", Hour: "*"},
        User:     productUser,
    }).
    Schedule(cron.CommandName(cli.NewProductListCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "0", Hour: "*/6"},
        User:     productUser,
    }).
    Schedule(cron.CommandName(cli.NewAppInfoCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "0", Hour: "12"},
    })
```

`cron.CommandName` is a generic helper that instantiates a constructor and returns the command name, so the schedule references commands by constructor instead of hardcoded strings.

Cron defaults (user, logs directory, destination file, template, heartbeat) come from the parameter system in [`config/parameter.go`](./config/parameter.go). The user is sourced from `APP_CRON_USER`, and the heartbeat is enabled via the `APP_CRON_HEARTBEAT_AUTO_ENABLED` opt-in (which auto-derives `<logs-dir>/heartbeat.crontab` from `melody.cron.logs_dir`) — both env vars live in [`.env`](./.env). [`config/cron.go`](./config/cron.go) reads `app.cron.product_user` (backed by `APP_CRON_PRODUCT_USER`) at registration time and applies it as the per-command user on the `catalog:report:refresh` and `product:list` schedules, demonstrating how the parameter cascade feeds custom values into `cron.Configuration` entries. The third entry, `app:info`, declares no user and falls back to `melody.cron.user`.

### Live integrations: database, cache and clock

Beside cron, the example wires [`integrations/bunorm`](../integrations/bunorm/) with its
[mysql provider](../integrations/bunorm/mysql/), [`integrations/rueidis`](../integrations/rueidis/) with its
[cache backend](../integrations/rueidis/cache/), and the framework's own [`clock`](../clock/).

Each one is gated on an endpoint parameter, and an unset endpoint leaves the integration unwired: no service,
nothing dialled, and the nomenclature falls back to what it can reach inside the process. That is what keeps the
example bootable with no containers at all. The endpoints ship in [`.env`](./.env) pointing at the
docker-compose service names, and a configured-but-unreachable endpoint is a warning rather than a boot failure,
so `go run .` works on a laptop.

None of the integrations has a route of its own. Each one carries a function of the nomenclature instead, so
what it does is visible in what the application does:

| integration | what carries it |
|---|---|
| `bunorm` + mysql provider | products, categories, currencies and users are kept in mysql when one is configured, and in memory when it is not |
| `rueidis` cache backend | the product listing is served from redis and dropped from it on every write |
| `rueidis` rate limiter | the nomenclature's write endpoints share a per-address budget; the reads stay open |
| `clock` | every write is stamped by the injected clock rather than by the wall, in the services and in the timing middleware |

`GET /catalog/report/` is the one endpoint kept: the clock-stamped catalogue reading, served from the cache
once the scheduled refresh has warmed it, and computable with no backend at all.

The `catalog:journal` command prints the latest entries of the write journal over the same repository the
listeners write it through, so the record is reachable from the command line as well as from a request.

Two details are worth reading in the source rather than guessed at:

- [`config/bootstrap_resolver.go`](./config/bootstrap_resolver.go) explains why an integration provider cannot
  resolve the configuration through the application's own container while the modules are being wired: boot runs
  the module hooks before it registers the framework's services. The database sidesteps it by opening lazily;
  redis cannot, because the rate-limit middleware is handed a live limiter at the moment a route is declared.
- The clock is injected into [`config/middleware.go`](./config/middleware.go) rather than read from the wall.
  That is what makes the `X-Example-Duration-Ms` header assertable at all — a frozen clock advanced by the
  handler underneath lets [`config/middleware_test.go`](./config/middleware_test.go) demand an exact value,
  which no test can do against `time.Now`.

### [`main.go`](./main.go) (why it stays small)

`main.go` intentionally contains minimal logic.

It only:

- opens the signal context, which is what gives the process its graceful-shutdown window on the first `SIGINT` or `SIGTERM` — a second signal during a hung shutdown forces it down
- constructs the Melody application using:
    - `embeddedEnvFiles` (from `embedded_env_*`)
    - `embeddedPublicFiles` (from `embedded_static_*`)
- calls `config.Configure(app)`
- runs the application under that context

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

The application also answers `GET /health` without a session, which is the route a monitoring system or a
container orchestrator probes. It is public on purpose: everything else in the example falls under the
`^/` catch-all rule of [`config/security.go`](./config/security.go), so a probe that had to authenticate
would be answered with a redirect to the login page instead of the readiness of the process.

### Frontend bundle

The pages are server-rendered HTML driven by a small TypeScript bundle: every link, form action and `fetch` target is built from a **route name** rather than a hardcoded path, against the manifest the application injects into each page as `window.melodyRoutes` (assembled in [`url/route_registry.go`](./url/route_registry.go)).

Everything a browser needs beyond the HTML is **generated, not committed**, and one command produces all of it:

```bash
cd .example/assets
npm ci
npm run build
```

That command does two things:

- `sync-icons.mjs` copies the shared icons out of `<repository root>/.assets`, which is the **single place** they exist in the tree, renaming each to the name the pages link it by: `favicon.ico` → `public/favicon.ico`, `logo.svg` → `public/assets/favicon.svg`, and `logo.png` → both `public/assets/logo.png` and `public/assets/apple-touch-icon.png`;
- esbuild bundles `assets/app.ts` into `public/assets/app.js`.

Both destinations are git-ignored, so a checkout carries no copy of either and neither can go stale. Without this step the pages still load and every JSON endpoint still answers, but `public/assets/app.js` and the four icons are 404s and the browser interface does nothing: logging in, listing, editing and deleting all go through `window.melodyExample.*`, which the bundle is what installs.

The development container runs the same script for every major at startup, so `./dc up:all` needs nothing extra.

While iterating on the TypeScript, `npm run build:watch` rebuilds on save, `npm run typecheck` runs `tsc --noEmit` over the sources, and `npm run sync-icons` re-copies the icons alone after a change in `.assets`.

### CLI mode

The example also wires CLI commands. List them:

```bash
cd .example
go run . -h
```

Among the commands you will find `melody:cron:generate` from the cron integration. To generate a crontab fragment from the `cron.Configuration` declared in [`config/cron.go`](./config/cron.go):

```bash
cd .example
go run . melody:cron:generate --out ./generated_conf/cron/crontab
```

The example schedules three commands in [`config/cron.go`](./config/cron.go) (`catalog:report:refresh` hourly, `product:list` every 6 hours, `app:info` daily at noon) plus a heartbeat enabled via `APP_CRON_HEARTBEAT_AUTO_ENABLED=true` in [`.env`](./.env) (the path is auto-derived from `melody.cron.logs_dir`), so the generated crontab is not empty.

---

## API response envelope

Every JSON endpoint answers through the same envelope, built in [`presenter/error_presenter.go`](./presenter/error_presenter.go):

- `success` — a boolean, so a caller branches on the envelope rather than on the status code
- `payload` — the answer itself, `null` on a failure
- `errors` — a list of messages, empty on success rather than absent
- `context` — present only when the kernel environment enables debug material
- `trace` — likewise

The envelope is the reason a client never decodes straight into the answer type: a decode that skipped it would read a failure as a zero value. The end-to-end harness unwraps it in `decodeExampleData` for exactly that reason.

The two representations are negotiated: a client that asks for HTML gets the page or an HTML error, one that asks for JSON gets this envelope, and one whose `Accept` header refuses every representation the application can produce is answered `406` rather than being handed JSON it said it would not take.

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

> **Run `cd assets && npm ci && npm run build` first.** Neither the frontend bundle nor the four shared icons is committed — they are produced by that one command, into a git-ignored `public/` (see [Frontend bundle](#frontend-bundle)). Under `melody_static_embedded` the `public/` directory is frozen into the binary at compile time (`//go:embed all:public`), so a binary built before that step carries neither and cannot gain them afterwards; under the filesystem modes the shipped `public/` is missing them just the same.

### A) Fully embedded “black-box” binary (recommended for a self-contained handout)

Build:

```bash
go build -tags "melody_env_embedded melody_static_embedded" -o example-app .
```

Ship:

- `example-app` binary

Required at runtime:

- nothing else — the binary carries every `.env` file and every asset that was in `public/` **at the moment `go build` ran**, which is why the frontend build above has to come first

---

### B) External configuration, embedded static assets

Build:

```bash
go build -tags "melody_static_embedded" -o example-app .
```

Ship:

- `example-app` binary
- `.env` file (s)

Required at runtime:

- `.env` file (s)

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
- `.env` file (s)
- [`public/`](./public/) directory

Required at runtime:

- `.env`
- [`public/`](./public/)

---

## Notes

- This example is intentionally compact and optimized for readability.
- Treat it as a **reference implementation** for Melody wiring patterns, not as a stable API contract.
- The framework APIs demonstrated here are authoritative; the example itself may evolve freely.
