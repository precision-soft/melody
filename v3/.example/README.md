# Melody Example Application (`.example`)

The `.example` directory contains a small **product catalog** application built as a **feature showcase** for the Melody framework.

It is **not** a full production product. Its purpose is to demonstrate how Melody is intended to be used in userland, with realistic wiring and clear architectural boundaries: routing, HTTP handlers, dependency injection, structured logging, sessions and authentication, security access control, caching, events, and CLI commands.

This README is the whole of the application's documentation. It keeps no changelog, because it has no history to keep: an example is not a project with a past, it has one state — the present one — and this document describes that state. A database left in an older shape is brought to it by `example:db:reset` rather than by a record of how it got there.

---

## What it represents

Conceptually, the example models a minimal admin-style catalog application:

- Product listing and detail pages
- Login / logout flow based on sessions
- A simple role system (`ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`)
- HTML pages backed by JSON endpoints (consumed via jQuery)
- CLI commands that demonstrate Melody’s CLI conventions and container/runtime usage
- Prices quoted in one currency and readable in another, against exchange rates fetched from an outside provider

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
├── cache/            # cache serializer for the example container
├── cli/              # the application's own CLI commands (app:info, product:list, catalog:report:refresh, messagebus:dispatch, auth:token, internal:sign, totp:code, mail:send, example:grant:role, example:exclusive:tick, example:db:reset)
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
├── url/              # route registry adapters: the route manifest every page is given
├── public/           # static assets (CSS / JS)
├── embedded_*.go     # build-tag–controlled embedding for env and static assets
├── main.go           # application entry point
├── go.mod / go.sum   # standalone module manifest
├── .env              # example env defaults
└── .gitignore
```

### [`config/`](./config/) — application wiring

The [`config/`](./config/) package keeps [`main.go`](./main.go) small by grouping all setup and integration logic in a single place, with each module hook in its own file:

- [`configure.go`](./config/configure.go) — entry point invoked by `main.go`: registers the example module and the integration module facades (observability, otlp, encrypt, outbox, migrate, cron, websocket, awss3, rueidis), each gated on the configuration that enables it
- [`module.go`](./config/module.go) — `Module` struct + `Name()` + `Description()` + interface assertions for the module hooks the example implements
- [`security.go`](./config/security.go) — `RegisterSecurity`: access-control rules, role hierarchy, decision manager, firewall
- [`http.go`](./config/http.go) — `RegisterHttpRoutes`: named-route registration for pages and JSON APIs
- [`cli.go`](./config/cli.go) — `RegisterCliCommands`: the application's own CLI commands; `melody:cron:generate` comes from the cron module registered in [`configure.go`](./config/configure.go)
- [`event.go`](./config/event.go) — `RegisterEventSubscribers`: wires the example's domain event subscribers
- [`parameter.go`](./config/parameter.go) — `RegisterParameters`: registers `melody.cron.*` parameters from `APP_CRON_*` env vars plus the example's own `app.*` parameters
- [`service.go`](./config/service.go) — `registerServices`: container wiring for repositories, services, and the cache serializer
- [`middleware.go`](./config/middleware.go) — example-specific HTTP middleware (`NewTimingMiddleware`)

### Cron integration

The example demonstrates Melody's [`integrations/cron`](../../integrations/cron/v3/) package. Commands stay plain Melody CLI commands — there is no `cron.Metadata` interface to implement. Schedules are declared separately in [`config/cron.go`](./config/cron.go) through a `cron.Configuration` registry:

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

The application also answers `GET /health` without a session, which is the route a monitoring system or a container orchestrator probes. It is public on purpose: everything else in the example falls under the
`^/` catch-all rule of [`config/security.go`](./config/security.go), so a probe that had to authenticate would be answered with a redirect to the login page instead of the readiness of the process.

> The committed [`.env`](./.env) points the integration endpoints at the dev compose service names (`redis:6379`, `mysql`, …), so the fully-wired experience is [`./dc up:all --build`](#running-fully-against-containers), which runs this same app inside the dev container where those names resolve. A bare host `go run .` needs those services reachable (override the endpoints to the mapped host ports, [see below](#running-the-binary-directly-against-mapped-ports)) — or remove their lines from `.env` to boot with the in-process fallbacks and zero infrastructure.

### Frontend bundle

The pages are server-rendered HTML driven by a small TypeScript bundle: every link, form action and `fetch` target is built from a **route name** rather than a hardcoded path, against the route manifest — the same document [`melody:routes:manifest`](#cli-mode) exports, injected into each page as `window.melodyRoutes`.

Everything a browser needs beyond the HTML is **generated, not committed**. The dev container produces it at startup, so `./dc up:all --build` needs nothing extra; a bare host run needs one command:

```bash
cd v3/.example/assets
npm ci
npm run build
```

That command does two things:

- `sync-icons.mjs` copies the shared icons — `favicon.ico`, `assets/favicon.svg`, `assets/logo.png`, `assets/apple-touch-icon.png` — out of `<repository root>/.assets`, which is the **single place** they exist in the tree;
- esbuild bundles `assets/app.ts` into `public/assets/app.js`.

Both destinations are git-ignored, so a checkout carries no copy of either and neither can go stale. Without this step the pages still load and every JSON endpoint still answers, but `public/assets/app.js` and the four icons are 404s and the browser interface does nothing: logging in, listing, editing and deleting all go through `window.melodyExample.*`, which the bundle is what installs.

While iterating on the TypeScript, `npm run build:watch` rebuilds on save, `npm run typecheck` runs `tsc --noEmit` over the sources, and `npm run sync-icons` re-copies the icons alone after a change in `.assets`.

### CLI mode

The example also wires CLI commands. List them:

```bash
cd v3/.example
go run . -h
```

Among the commands you will find `melody:cron:generate` from the cron integration. To generate a crontab fragment from the `cron.Configuration` declared in [`config/cron.go`](./config/cron.go):

```bash
cd v3/.example
go run . melody:cron:generate --out ./generated_conf/cron/crontab
```

The example schedules three commands in [`config/cron.go`](./config/cron.go) (`catalog:report:refresh` hourly, `product:list` every 6 hours, `app:info` daily at noon) plus a heartbeat enabled via `APP_CRON_HEARTBEAT_AUTO_ENABLED=true` in [`.env`](./.env) (the path is auto-derived from `melody.cron.logs_dir`), so the generated crontab is not empty.

The same `cron.Configuration` also drives an **in-process scheduler** for single-binary deployments with no external crontab. `melody:cron:run` ticks in-process and invokes each scheduled command when it is due; `--once` evaluates every schedule against the current time, runs the due commands and exits:

```bash
cd v3/.example
go run . melody:cron:run --once      # kick whatever is due now, then exit
go run . melody:cron:run             # run the scheduler loop until interrupted
```

The runner dispatches each scheduled command with its declared flags, so declared defaults are honored on a scheduled tick exactly as under the cli entry point: `product:list` declares `--limit` with a default of `5` and prints the value it read (`product list: limit=5`), whether invoked directly or by the runner.

`example:grant:role` shows that an application command may declare its own `--role` flag: the runtime's `--role`/`--mode` are recognized only before the command name, so the command receives its flag intact. It also holds the example's user service through a `container.Lazy` handle built at command-registration time — the service is resolved at the command's first run, not during the boot phase:

```bash
go run . example:grant:role --role admin --user ada    # the command's own --role
go run . --role worker app:info                        # the runtime process role
```

---

## Platform integrations (optional, env-gated)

The example wires **every v3 platform integration**. Each backend that needs external infrastructure is **gated on an environment variable**: when the variable is unset the application boots with an in-process fallback (remove the integration lines from [`.env`](./.env) and the example runs with zero infrastructure), and when it is set the matching integration is activated and resolved through the same core service constant, so the rest of the app is unchanged. The committed `.env` sets these variables to the dev compose service endpoints so `./dc up:all` wires everything out of the box.

| Integration                                                                                                                                      | Activated by                                 | Falls back to            | Reached at                                    |
|--------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------|--------------------------|-----------------------------------------------|
| [`opentelemetry`](../../integrations/opentelemetry/v3/) — Prometheus metrics middleware                                                          | always on                                    | —                        | `GET /metrics`                                |
| [`websocket`](../../integrations/websocket/v3/) — WebSocket bound to the SSE hub                                                                 | always on                                    | —                        | `GET /ws` (and `GET /events/stream` for SSE)  |
| `encrypt` ([`bunorm/v3/encrypt`](../../integrations/bunorm/v3/encrypt/)) — AES-256-GCM cipher                                                    | always on                                    | —                        | `GET /encrypt/roundtrip`                      |
| [`amqp`](../../integrations/amqp/v3/) — durable message-bus transport                                                                            | `AMQP_DSN`                                   | in-memory transport      | `POST /messagebus/dispatch`                   |
| [`awss3`](../../integrations/awss3/v3/) — S3 object storage (`storage.ServiceStorage`)                                                           | `S3_ENDPOINT`                                | local filesystem storage | `GET /platform/check`                         |
| [`rueidis`](../../integrations/rueidis/v3/) — Redis cache backend, distributed lock (`lock.ServiceLocker`), revocable token store, SSE backplane | `REDIS_ADDRESS`                              | in-memory cache/lock     | `POST /access-token/issue/`                   |
| [`bunorm/mysql`](../../integrations/bunorm/mysql/v3/) — MySQL `GET_LOCK` distributed lock                                                        | `MYSQL_HOST` (when `REDIS_ADDRESS` is unset) | in-memory lock           | `GET /platform/check`                         |
| [`bunorm`](../../integrations/bunorm/v3/) — bun ORM `*bun.DB`, transparent column encryption, field-level audit trail                            | `MYSQL_HOST`                                 | —                        | every catalogue write: audited and journalled |
| [`bunorm/migrate`](../../integrations/bunorm/migrate/v3/) — the `db:*` migration command family                                                   | always on (fails at `Run` without a database) | —                        | `db:migrate`, `db:status`, `db:rollback`      |

The lock service follows a single priority: Redis if configured, otherwise MySQL, otherwise in-memory. Transparent encryption-at-rest is not shown through a route of its own: the two-factor enrollment table stores the shared secret and the recovery codes as `encrypt.EncryptedString` columns, so `POST /twofactor/enroll` writes ciphertext MySQL holds and `POST /twofactor/verify` reads it back to recompute a code. Both act on the caller's own account — the identifier comes from the authenticated token rather than from the request — and enrolling again replaces the enrollment that is there, which is the door an account whose authenticator is lost needs. `GET /encrypt/roundtrip` reports the ciphertext beside the value that came back, which is the half a stored column cannot show.

Several wirings deliberately defer their resolution to first use instead of the composition root:

- `example:exclusive:tick` is wrapped in `lock.NewExclusiveCommand` over `lock.NewLazyLocker`, which resolves the registered locker at the first `CreateLock` — with a distributed locker configured (Redis or MySQL), run it from two shells at once and exactly one executes while the other exits zero. Under the in-memory fallback the exclusivity is per-process, so two separate shells both execute.
- The transactional-outbox module ([`config/outbox.go`](./config/outbox.go)) is registered in the `StoreFactory`/`RelayFactory` shape: the store (which ensures the `melody_outbox` schema) and the relay (which opens the transport) are built from the container at first use, and the module contributes the `melody:outbox:relay` command over the same lazily-resolved relay. Endpoints: `POST /outbox/enqueue`, `POST /outbox/relay`, `GET /outbox/status`.
- The encrypt module resolves the shared `*bun.DB` through a `DatabaseFactory` evaluated at the first `melody:encrypt:database` run, so http- and worker-mode processes register the command without touching the database.
- The message-bus transport ([`config/messagebus.go`](./config/messagebus.go)) is handed a dialer and nothing else, the rule its outbox twin states in the same words: the transport closes only a connection it dialed itself, so one opened in the composition root would be owned by nobody. Nothing dials at boot — a process that never publishes never opens a connection, and `db:migrate --help` no longer pays a full amqp handshake before printing its usage. An `AMQP_DSN` this application cannot reach therefore does not stop the boot: it surfaces at the first publish, through the transport's own retry loop, which is where a broker that is merely down already surfaced.

### Exchange rates — the outbound half

The catalogue quotes every product in one currency, so a reader who wants another one needs a rate. That is
what the currency nomenclature carries beside the code, and it is the only thing in this repository that
calls [`httpclient`](../httpclient): the package has no other consumer on any major, so before this the whole
of it was proven by compilation.

- `example:currency:refresh-rates` reads the provider named by `RATES_BASE_URL` and writes the quotes it
  recognises. It runs on the half hour from [`config/cron.go`](./config/cron.go) and exits **non-zero** when
  the provider could not be read — a schedule that swallowed that would leave the catalogue quoting stale
  rates in silence. With the key blank the command is a no-op that says so and exits zero, the same switch
  every optional door here carries.
- `GET /products/api/read/:id/?currency=USD` answers the product with its price restated in the currency the
  caller named, stamped with the instant of the quote it used. Without the parameter the answer carries no
  conversion at all; a code the catalogue does not carry is a `400`.
- `catalog:report:refresh` pushes its reading to `APP_REPORTING_EXPORT_ENDPOINT` when one is configured, and
  takes the command's exit code with it if the sink refuses.

The two endpoints are the two SHAPES the client supports, and they are not interchangeable. `RATES_BASE_URL`
is a **base** and carries its trailing slash: the client resolves every target against it by RFC 3986, so the
slash is what keeps `/v1` a prefix instead of a segment the merge cuts, and a base written without one is
refused where the wiring mistake is made. A based client also refuses a target that resolves off its origin,
which is why the export endpoint — a whole url an operator may point at any host — travels through a second
client that carries no base at all. Both are registered by NAME and not by type: they are the same concrete
type, so a resolution by type could only answer with whichever landed first, and the boot refuses the second
registration outright.

The rate is quoted against a base — one unit of that base costs `rate` units of the currency — so a
conversion between two currencies cancels it and the catalogue never has to know what the base was. The
instant stored is the PROVIDER's rather than the moment the refresh ran, because a reader deciding whether a
price is stale needs the age of the reading. In development the provider is a vhost of the compose load
balancer at `rates.melody.localhost.precision-soft.com`, which also serves the failure arms the bands drive.

### The migration set

The schema is owned by one migration set in [`migration/`](./migration/) — a single MySQL DDL migration holding the six tables this example owns and the one constraint it declares: the four catalogue tables, the journal, the two-factor enrollment table neither frozen major carries, and the unique key on the folded spelling of a username, which is what holds a name against two callers that pass the repository's read-then-write check at the same moment. The set is one migration because this application has no history — an example has one state, the present one, so its schema is the statement of that state rather than the record of how it got there, and a database left in an older shape is answered by `example:db:reset` rather than by a step that repairs its past. This is the catalogue's set, on the catalogue's connection; the reading archive has a set and a command family of its own, described below. Two doors run the set, so neither can drift from the other:

- the **composition root and the repository constructors** call `migration.EnsureMigrated`. `buildTwoFactor` calls it at boot — this example dials eagerly, so the schema is in place before the first request rather than at the first resolution the way v1 and v2 do it — and each repository constructor calls it again before seeding, which is what keeps a freshly recreated volume usable with no operator step. It is also why every `CREATE TABLE` carries `IF NOT EXISTS`: several processes of the example may apply the set at the same time, serialized by bun's migration lock with a bounded retry that names `db:unlock` when it gives up;
- the **`db:*` command family** (`db:init`, `db:migrate`, `db:rollback`, `db:status`, `db:unlock`, `db:create`) runs the same set from the operator's side. It comes from the [`integrations/bunorm/migrate`](../../integrations/bunorm/migrate/v3/) module facade registered in [`config/configure.go`](./config/configure.go), pinned to the example's own manager registry service.

`example:db:reset` is the third door, and the only one that goes backwards. It drops the tables the set owns, drops and recreates the bun bookkeeping with them, applies the schema again and reseeds every nomenclature in one pass. It exists because this application has no history: a database left in an older shape — carrying bookkeeping rows that name migrations this schema no longer has — is brought to the present state here, by a command an operator runs deliberately, rather than by code every process pays for at boot. It refuses to act without `--force`, printing what it would drop and exiting zero, and it is a command of the application rather than of the migration module: dropping an application's whole schema is not an operator door a published module should grow. The audit trail is emptied with the rest, though its table is not dropped: the schema belongs to the module that opens it, the rows belong to this application, and a trail carried across a reset would name entities that no longer exist over identifiers this example recycles.

The connection itself is declared in a `bunorm.ManagerRegistry` ([`config/database.go`](./config/database.go)) rather than opened directly: the registry is the one door the commands resolve, and the `*bun.DB` the rest of the application holds is its default manager. Closing the registry is what closes the pool — and what puts it in reach of the ordered teardown is that the catalogue storage RESOLVES the handle rather than holding the one the composition root built, because the container records a dependency at the moment one provider resolves another. That same resolution is what installs the application's journal on the registry, so the pool's reporting and bun's diagnostics leave the emergency logger at the first repository resolution instead of never.

The module is registered whether or not a database is configured, so the command surface does not change between environments; without one every `db:*` command fails at `Run` with the container refusal naming the registry service.

The framework's own tables are not in the set: the outbox store and the audit registry each open their schema through the module that owns it, and a set belonging to the application would claim tables it does not own.

The set lives in a database of this major's own — `melody_example_v3`, named in [`.env`](./.env). The three example applications share the development MySQL and not a database in it: the bun bookkeeping tables (`bun_migrations`, `bun_migration_locks`) keep their default names, and bun matches an applied migration by name, so on one shared database the three sets would share one bookkeeping table and the first to land would answer for the others.

### The reading archive — the second database

This example holds **two** databases: the catalogue on MySQL and an archive of catalogue readings on PostgreSQL. They are two independent switches — `MYSQL_HOST` and `PGSQL_HOST`, each empty-means-unwired — so all four combinations boot: both live, either one alone, or neither.

The archive is what the scheduled report leaves behind. `catalog:report:refresh` takes a reading of the catalogue, leaves it in the cache and pushes it to the export endpoint; it now also records it, one row per reading, so `GET /reports/api/history/` can answer how the catalogue has moved. The listing carries the same role the catalogue listings carry — a reading is the catalogue counted, so whoever may not read the nomenclature may not read its history either — and takes a `limit` the door caps, because the archive grows for the life of a volume.

Three things about it are worth reading rather than inferring:

- **A reading's identity is the instant it was taken at, to the second.** That is the resolution the reading already states about itself: its payload writes `recorded_at` as RFC3339, which carries no fraction, so a key kept finer would disagree with the very value it keys. The instant is the table's primary key, so two refreshes inside one second are the same reading — and the second one is told it did not write rather than being failed.
- **The archive is written by the SCHEDULE, not by a request.** A reading is taken on the request path too, whenever a caller finds a cold cache; archiving there would put a write to a second database on a read, make a read door fail when PostgreSQL is down, and fill the archive with rows nobody scheduled.
- **The write is taken under a PostgreSQL advisory lock** ([`pgsql.NewLocker`](../../integrations/bunorm/pgsql/v3/lock.go)), registered under a name of its own rather than the framework's locker service — that one is Redis when Redis is configured, and the archive is not on Redis. A lock held in the very database being written is exclusion that cannot disagree with the write it guards, and a session advisory lock is released when its connection drops, so a process that dies mid-refresh leaves nothing to clean up. Several processes running the same schedule record one reading between them rather than one each.

The archive's schema is a set of its own, in the same package, and it has to be: `bun_migrations` is per database, so two databases need two sets and one set could never span them. It is exposed as a migration **context** of the `bunorm/migrate` module, which gives it the `db:archive:*` command family (`db:archive:migrate`, `db:archive:status`, `db:archive:rollback`, `db:archive:unlock`, …) pinned to its own manager — so `db:archive:migrate` can only ever reach PostgreSQL. The base `db:*` family is pinned to the catalogue's manager for the same reason, and that pin is not symmetry: unpinned it takes the registry's default, and in an environment that wired the archive alone an unqualified `db:migrate` would aim the catalogue's MySQL DDL at PostgreSQL.

`example:db:reset` covers both databases, and names both in the plan it prints before it destroys anything. An environment that wired no archive is not told its reset failed over a database it never asked for: that half is skipped, and the plan stays silent about it.

The archive database is this major's own too — `melody_example_v1` and `melody_example_v3` are created on the development PostgreSQL by [`init.sql`](../../.dev/docker/postgres/init.sql) on a fresh volume, and idempotently by the e2e harness for a volume that predates it. `melody_test` stays behind for the live integration suites.

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
- **Always `up:all`, not plain `up`.** The backends live on the compose `all` profile, so `./dc up` / `./dc up:minimal` start only the dev container and load balancer — the example would then have no Redis/MySQL to reach. Use `./dc up:all` whenever you want the live integrations.
- **Cold-start is self-healing.** If a backend is not ready yet — or you start it afterwards — the MySQL/Redis providers retry the initial connection with backoff and the entrypoint supervisor restarts the process, so the app comes up on its own without a manual restart.
- **A proxy in front has to forward the host the browser asked for.** The websocket module is wired with no origin patterns, so the library's same-origin default is what stops a foreign page from riding a signed-in visitor's session cookie onto the feed — and that default compares the browser's Origin header with the Host the application was handed. An nginx that forwards the server name rather than the raw Host strips the port, so on any published port but 80 the two disagree and every upgrade is refused with 403, which reads exactly like the refusal a foreign origin gets. The dev load balancer forwards the raw Host; a deployment behind a different proxy has to do the same, or name its own allowed origins.

The endpoints listed above then work against the mapped host port, e.g. `curl localhost:8180/platform/check`, `curl localhost:8180/encrypt/roundtrip`.

#### Running the binary directly against mapped ports

To run `go run .` on the host (outside the dev container) instead, point the integrations at the **mapped host ports** — an OS environment variable overrides the matching `.env` value:

```bash
cd v3/.example
AMQP_DSN="amqp://guest:guest@localhost:5673/" \
REDIS_ADDRESS="localhost:6380" \
S3_ENDPOINT="localhost:4566" S3_ACCESS_KEY="test" S3_SECRET_KEY="test" S3_BUCKET="melody-example" \
MYSQL_HOST="localhost" MYSQL_PORT="3307" MYSQL_DATABASE="melody_example_v3" MYSQL_USER="melody" MYSQL_PASSWORD="melody" \
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
