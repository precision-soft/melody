# Melody Example Application (`.example`)

The `.example` directory contains a small **product catalog** application built as a **feature showcase** for the Melody framework.

It is **not** a full production product. Its purpose is to demonstrate how Melody is intended to be used in userland, with realistic wiring and clear architectural boundaries: routing, HTTP handlers, dependency injection, structured logging, sessions and authentication, security access control, caching, events, and CLI commands.

---

## What it represents

Conceptually, the example models a minimal admin-style catalog application:

- Product listing and detail pages
- Login / logout flow based on sessions, with an api-key firewall beside it for machine clients
- A simple role system (`ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`)
- HTML pages backed by JSON endpoints (driven by the TypeScript bundle in `assets/`)
- CLI commands that demonstrate Melody’s CLI conventions and container/runtime usage

---

## Seeded credentials

For convenience, the example ships with a few predefined users:

- `user` / `user` — `ROLE_USER`
- `editor` / `editor` — `ROLE_USER`, `ROLE_EDITOR`
- `admin` / `admin` — `ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`

Passwords are stored as **bcrypt hashes** ([`security/password_hasher.go`](./security/password_hasher.go)):
`security.HashPassword` at seeding and in the user handlers, `security.PasswordMatches`
(`bcrypt.CompareHashAndPassword`) at login. The hash is salted, so the same password produces a different
stored value on every boot; the credentials above are the stable contract, not the bytes in the table.

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
├── migration/        # the migration set that owns the database schema: five DDL migrations plus the first-resolution door the repositories run
├── page/             # HTML page templates
├── presenter/        # HTTP error / response presenters
├── repository/       # repository interfaces with an in-memory and a database-backed implementation each, plus the seed data and the helpers both share
├── route/            # named route constants and patterns
├── security/         # session auth wiring (login/logout handlers, entry point, access denied handler, token resolver, password hasher)
├── service/          # application services (Category, Currency, Product, User, plus the catalog journal, the request-scoped change attribution and the catalog report)
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

- [`configure.go`](./config/configure.go) — entry point invoked by `main.go`: registers the example module, whose hooks below contribute everything else, plus two integration module facades — the cron module (which registers `melody:cron:generate` and the in-process `melody:cron:run`) and the bunorm/migrate module (which registers the `db:*` command family over the example's migration set)
- [`module.go`](./config/module.go) — `Module` struct + `Name()` + `Description()` + interface assertions for the module hooks the example implements
- [`security.go`](./config/security.go) — `RegisterSecurity`: access-control rules, role hierarchy, decision manager, and the two firewalls — the stateless api-key firewall on `/products/api` and the session firewall behind it
- [`http.go`](./config/http.go) — `RegisterHttpRoutes`: named-route registration for pages and JSON APIs, plus the forwarded-headers trust policy the kernel and the rate limiter share
- [`cli.go`](./config/cli.go) — `RegisterCliCommands`: the example's own CLI commands; the cron and `db:*` commands come from the two module facades registered in `configure.go`
- [`event.go`](./config/event.go) — `RegisterEventSubscribers`: wires the example's domain event subscribers and the cors listeners when `APP_CORS_ALLOW_ORIGINS` names any origin
- [`parameter.go`](./config/parameter.go) — `RegisterParameters`: registers `melody.cron.*` parameters from `APP_CRON_*` env vars plus the example's own `app.*` parameters, the showcase switches included
- [`service.go`](./config/service.go) — `registerServices`: container wiring for repositories, services, the cache serializer, and the file-backed session storage when `APP_SESSION_FILE` names a file
- [`middleware.go`](./config/middleware.go) — example-specific HTTP middleware (`NewTimingMiddleware`) plus the framework's `CompressionMiddleware`
- [`cron.go`](./config/cron.go) — the `cron.Configuration` both cron commands share (which command runs on which schedule, and as which system user), plus `cronRunnerCommands`, the instantiated commands the in-process runner invokes
- [`database.go`](./config/database.go) — the bun manager registry and the MySQL provider, built only when the configuration published a host; an unset host leaves the registry nil and every repository falls back to its in-memory twin
- [`redis.go`](./config/redis.go) — the rueidis client, the cache backend bound to `cache.ServiceCacheBackend`, and the rate limiter the write routes are put behind; an unset address leaves all three absent
- [`bootstrap_resolver.go`](./config/bootstrap_resolver.go) — reads a parameter before the container exists, which is what lets the two files above decide whether to wire an integration at all

### Cron integration

The example demonstrates Melody's [`integrations/cron`](../integrations/cron/) package through its module facade, registered in [`config/configure.go`](./config/configure.go):

```go
app.RegisterModule(cron.NewModule(cron.ModuleConfig{
    ConfigurationFactory: newCronConfiguration,
    RunnerCommands:       cronRunnerCommands(),
}))
```

The module registers two commands over one shared `cron.Configuration`: `melody:cron:generate`, which renders the crontab manifests, and `melody:cron:run`, the in-process runner that invokes each scheduled command when its minute comes (`--once` evaluates a single tick, `--report-idle` also reports the minutes that dispatch nothing). Commands stay plain Melody CLI commands — there is no `cron.Metadata` interface to implement. Schedules are declared separately in [`config/cron.go`](./config/cron.go):

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

`cron.CommandName` is a generic helper that instantiates a constructor and returns the command name, so the schedule references commands by constructor instead of hardcoded strings; `cronRunnerCommands` hands the same three constructors, instantiated, to the runner. The `ModuleConfig.WithDefaultParameters` field stays unset because [`config/parameter.go`](./config/parameter.go) registers the `melody.cron.*` parameters with the example's own values.

Cron defaults (user, logs directory, destination file, template, heartbeat) come from the parameter system in [`config/parameter.go`](./config/parameter.go). The user is sourced from `APP_CRON_USER`, and the heartbeat is enabled via the `APP_CRON_HEARTBEAT_AUTO_ENABLED` opt-in (which auto-derives `<logs-dir>/heartbeat.crontab` from `melody.cron.logs_dir`) — both env vars live in [`.env`](./.env). [`config/cron.go`](./config/cron.go) reads `app.cron.product_user` (backed by `APP_CRON_PRODUCT_USER`) at registration time and applies it as the per-command user on the `catalog:report:refresh` and `product:list` schedules, demonstrating how the parameter cascade feeds custom values into `cron.Configuration` entries. The third entry, `app:info`, declares no user and falls back to `melody.cron.user`.

### Live integrations: database, cache and clock

Beside cron, the example wires [`integrations/bunorm`](../integrations/bunorm/) with its
[mysql provider](../integrations/bunorm/mysql/) **and** its [pgsql provider](../integrations/bunorm/pgsql/) —
two live databases in one process, each with its own function — plus
[`integrations/rueidis`](../integrations/rueidis/) with its
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
| `bunorm` + pgsql provider | the write journal is kept in postgres when one is configured, and absent when it is not — the writes still succeed, unjournalled. The two switches are independent (`MYSQL_HOST` and `PGSQL_HOST`), so all four combinations boot. The compose postgres speaks plain TCP, so `.env` arms `PGSQL_INSECURE=true`; the provider verifies the TLS handshake on any other value |
| `rueidis` cache backend | the product listing is served from redis and dropped from it on every write |
| `rueidis` rate limiter | the nomenclature's write endpoints share a per-address budget; the reads stay open |
| `clock` | every write is stamped by the injected clock rather than by the wall, in the services and in the timing middleware |

`GET /catalog/report/` is the one endpoint kept: the clock-stamped catalogue reading, served from the cache
once the scheduled refresh has warmed it, and computable with no backend at all.

The `catalog:journal` command prints the latest entries of the write journal over the same repository the
listeners write it through, so the record is reachable from the command line as well as from a request.

The journal is also where the example demonstrates the container's request scope and lazy resolution. Who is
behind a request's changes is resolved ONCE per request, by the scoped
[`service.ChangeAttribution`](./service/change_attribution.go) registered through the module's
`RegisterScopedServices` hook ([`config/scoped_service.go`](./config/scoped_service.go)); its provider refuses
any scope without a request context, which is how a console run — whose scope carries a process context
instead — falls back to per-call attribution through the same fold, so the verdicts cannot drift. The journal
service holds its repository as a [`container.Lazy`](../container/lazy.go) handle: nothing dials postgres
until the first recorded change, so the process boots and serves the catalogue with the journal database down.

Two details are worth reading in the source rather than guessed at:

- [`config/bootstrap_resolver.go`](./config/bootstrap_resolver.go) explains why an integration provider cannot
  resolve the configuration through the application's own container while the modules are being wired: boot runs
  the module hooks before it registers the framework's services. The database sidesteps it by opening lazily;
  redis cannot, because the rate-limit middleware is handed a live limiter at the moment a route is declared.
- The clock is injected into [`config/middleware.go`](./config/middleware.go) rather than read from the wall.
  That is what makes the `X-Example-Duration-Ms` header assertable at all — a frozen clock advanced by the
  handler underneath lets [`config/middleware_test.go`](./config/middleware_test.go) demand an exact value,
  which no test can do against `time.Now`.

### Security and HTTP showcase wirings

Beside the integrations, the example wires several framework doors that need no backend at all. Each follows
the same switch convention as the integrations — the value ships in [`.env`](./.env), and an empty or removed
value leaves that door unwired:

| wiring | switch | what it shows |
|---|---|---|
| api-key firewall | `APP_API_TOKEN` | a stateless firewall on `/products/api` ([`config/security.go`](./config/security.go)): `X-Api-Key` with the configured token authenticates as an editor-role client, no session involved. The firewall's matcher ([`security/api_key_request_matcher.go`](./security/api_key_request_matcher.go)) claims only requests that PRESENT the header, so browser cookie traffic keeps falling through to the session firewall on the same paths |
| cors listeners | `APP_CORS_ALLOW_ORIGINS` | the [`http/cors`](../http/cors/) LISTENERS rather than the middleware ([`config/event.go`](./config/event.go)): a preflight is answered before routing and before the security chain can refuse it, and the security refusals themselves carry the cors headers — responses the middleware chain never sees |
| compression | always on | the framework's `CompressionMiddleware` with its defaults ([`config/middleware.go`](./config/middleware.go)): gzip for bodies of at least a kilobyte, already-compressed media excluded, `Vary: Accept-Encoding` added |
| trusted proxies | always on | one `ForwardedHeadersPolicy` ([`config/http.go`](./config/http.go)) read by the http kernel for the scheme and by the rate limiter's client-ip resolver for the budget key, so a write arriving through a trusted proxy is budgeted against the `X-Forwarded-For` address rather than the proxy's |
| file-backed sessions | `APP_SESSION_FILE` | `session.NewFileStorageFromPath` registered under the framework's storage service id ([`config/service.go`](./config/service.go)), so a signed-in session survives a process restart; a relative path is anchored to the project directory. Empty keeps the framework's in-memory default |
| static cache | `MELODY_STATIC_ENABLE_CACHE` | the framework's static file server with its validators armed (`.env` ships `true` and `MELODY_STATIC_CACHE_MAX_AGE=3600`): assets answer with a strong `ETag`, `Last-Modified` and `Cache-Control: public, max-age=3600`, a replayed validator is answered `304`, and `If-None-Match` silences `If-Modified-Since`, as the precedence demands |

The session token resolver ([`security/session_token_resolver.go`](./security/session_token_resolver.go))
accepts the role list in the two spellings a session can carry: the `[]string` the login handler writes, and
the `[]any` the file storage answers after a restart — its snapshot round-trips through json, which keeps no
element type.

### Database migrations

The database schemas are owned by two migration sets in [`migration/`](./migration/), one per database: the
catalog set (`migration.Migrations`, four mysql DDL migrations, one per catalog table) and the journal set
(`migration.JournalMigrations`, one postgres DDL migration). Two doors run each set, so neither can drift from
the other:

- the **repository providers** call `migration.EnsureMigrated` (catalog) or `migration.EnsureJournalMigrated`
  (journal) at first resolution, and the catalog providers then seed an empty table. That is what keeps a
  freshly recreated volume usable with no operator step — the tables appear when the first request reaches a
  repository — and it is why every `CREATE TABLE` in both sets carries `IF NOT EXISTS`: several example
  processes share the databases and may apply a set at the same time, serialized by bun's migration lock with
  a bounded retry;
- the **`db:*` command family** (`db:init`, `db:migrate`, `db:rollback`, `db:status`, `db:unlock`, `db:create`)
  runs the catalog set, and the **`db:journal:*` context family** — same six verbs — runs the journal set;
  both come from the [`integrations/bunorm/migrate`](../integrations/bunorm/migrate/) module facade registered
  in [`config/configure.go`](./config/configure.go), pinned to the example's own manager registry service
  (`service.example.database.registry`). The base family is pinned to the catalog manager by name, so a
  journal-only environment refuses it cleanly instead of aiming mysql DDL at postgres.

The module is registered whether or not a database is configured, so the command surface does not change
between environments; without one every `db:*` command fails at `Run` with the container refusal naming the
registry service. The bun bookkeeping tables (`bun_migrations`, `bun_migration_locks`) keep their default,
major-unprefixed names in both databases — bookkeeping is per database, and within each one only the set that
lives there uses bun's migrator.

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

The example is a standalone Go module (`.example/go.mod`) that depends on Melody and its cron, bunorm (with the mysql and pgsql providers and the migrate command family) and rueidis integrations. From the repository root:

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

Among the commands you will find the cron pair (`melody:cron:generate` and `melody:cron:run`) and the two migration families (`db:*` for the catalog set on mysql, `db:journal:*` for the journal set on postgres) from the two module facades. To generate a crontab fragment from the `cron.Configuration` declared in [`config/cron.go`](./config/cron.go):

```bash
cd .example
go run . melody:cron:generate --out ./generated_conf/cron/crontab
```

The example schedules three commands in [`config/cron.go`](./config/cron.go) (`catalog:report:refresh` hourly, `product:list` every 6 hours, `app:info` daily at noon) plus a heartbeat enabled via `APP_CRON_HEARTBEAT_AUTO_ENABLED=true` in [`.env`](./.env) (the path is auto-derived from `melody.cron.logs_dir`), so the generated crontab is not empty. The same schedule drives the in-process runner:

```bash
cd .example
go run . melody:cron:run --once
```

The example's own commands (`app:info`, `product:list`, `catalog:journal`, `catalog:report:refresh`) render
through the framework's `cli/output` envelope, so each accepts the standard flag set
(`--format=table|json|json-pretty`, `--limit`, `--offset`, `--order`, `--quiet`, `--verbose`,
`--table-width`) and answers one machine-readable document under `--format=json`. For `catalog:journal` the
standard `--limit` replaces the flag the command used to declare itself: `0` (the default) answers every
entry, newest first.

---

## API response envelope

Every JSON endpoint answers through the same envelope, built in [`presenter/error_presenter.go`](./presenter/error_presenter.go):

- `success` — a boolean, so a caller branches on the envelope rather than on the status code
- `payload` — the answer itself, `null` on a failure
- `errors` — a list of messages, empty on success rather than absent. A validation refusal carries **one
  entry per violated field**, each spelled `field: message` (`presenter.ApiValidationError`), so a client
  attaches every message to the input that earned it instead of splitting a joined string
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
