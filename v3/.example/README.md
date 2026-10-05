# Melody Example Application (`.example`)

The `.example` directory contains a small **product catalog** application built as a **feature showcase** for the Melody framework.

It is **not** a full production product. Its purpose is to demonstrate how Melody is intended to be used in userland, with realistic wiring and clear architectural boundaries: routing, HTTP handlers, dependency injection, structured logging, sessions and authentication, security access control, caching, events, and CLI commands.

This README is the whole of the application's documentation. It keeps no changelog, because it has no history to keep: an example is not a project with a past, it has one state — the present one — and this document describes that state. A database left in an older shape is brought to it by `example:db:reset` rather than by a record of how it got there.

---

## What it represents

Conceptually, the example models a minimal admin-style catalog application:

- Product listing and detail pages
- Login / logout flow based on sessions
- A simple role system (`ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`) ordered by a role hierarchy: the admin is above the editor, the editor above the user
- HTML pages backed by JSON endpoints (consumed via jQuery)
- CLI commands that demonstrate Melody’s CLI conventions and container/runtime usage
- Prices quoted in one currency and readable in another, against exchange rates fetched from an outside provider

---

Sign-in and every signed-in request read the account from the repository, past the cached user records: a role granted or taken away applies on the next request, and a deleted account or a changed password ends the sessions opened before it, since a session carries a version of the password hash it was opened under. When the repository cannot be read, the request is answered as anonymous and the session is kept for the next one. The admin update and delete doors read the account from the repository as well, under the row's lock: an update writes only the fields its body names, so a password or a role list it leaves out keeps what the directory holds at that instant, and a grant landing beside it is never written back over; and the refusal of a peer administrator is decided on the account the write changes, never on a cached copy.

An account enrolled in a second factor (`POST /twofactor/enroll`) signs in with it: the login door authenticates through the framework's `AuthenticatorManager` over a `TotpSecondFactorAuthenticator`, whose first factor is the password. The password alone is answered `401` with `{"factor":"totp"}` in the payload and raises no login failure, since nothing was refused; the form then reveals a code field and posts the code on `X-2FA-Code`, or a single-use recovery code on `X-2FA-Recovery-Code`. A wrong, spent or replayed code is refused as a wrong password is, `invalid credentials` and one `security.login.failure`. An accepted code is burned in one memory the login door and `POST /twofactor/verify` share, in redis when the example has one, so a code is spent once across both. The authenticator does not limit guesses, so an account whose password was accepted may present five codes in fifteen minutes before even a right one is refused `429`; a caller without the password spends nothing of it, and an accepted code gives it back. An account with no enrollment signs in on its password, as before.

## Seeded credentials

In development (`MELODY_ENV=dev`, what `.env` ships) an empty user directory starts from three accounts:

- `user` / `user` — `ROLE_USER`
- `editor` / `editor` — `ROLE_EDITOR`
- `admin` / `admin` — `ROLE_ADMIN`

Each account holds the one role that names its position; the role hierarchy of [`config/security.go`](./config/security.go) is what lets the admin edit and the editor read, so a role added later is one line of the hierarchy rather than an edit of every account. Their passwords are in this README, so they exist in development only: in any other environment the directory starts empty, and its first account is created from the console (see below).

## Before production

Every credential this example needs is configuration, and `.env` commits a development value for each so a checkout boots as it is. Those values are public — anyone who read this repository holds them — so outside development (`MELODY_ENV` other than `dev`) the boot refuses a blank value or the committed one, naming the key and never the value:

- `APP_JWT_SECRET` — the HS256 secret the `/secure` api verifies bearer tokens with (and `auth:token` signs them with). Signed with the committed value, a token carrying `scope: {roles: ["ROLE_ADMIN"]}` would be accepted anywhere that value is;
- `APP_INTERNAL_AUTH_SECRET` — the shared secret of the HMAC envelopes the `/internal` firewall authenticates as `ROLE_SERVICE`, under the key id `APP_INTERNAL_AUTH_KEY_ID` for the caller `APP_INTERNAL_AUTH_APP` (those two name the caller and grant nothing; blank takes `wms-key-1` and `wms-service`);
- `APP_ENCRYPT_KEYS` — the keys the two-factor columns are encrypted under; without a list the development key is the one key in development only, and outside it a list holding either committed key is refused, naming the entry's position.

The seed accounts follow the same rule. Outside development the user directory starts empty, and an operator creates the first administrator from the console, the password read from standard input and never from an argument, where the shell history and the process list would keep it:

```bash
printf '%s\n' "$ADMIN_PASSWORD" | ./app example:user:create --role ROLE_ADMIN ada
```

`example:user:create` writes through the user service, so the account has its insert entry in the audit trail and the created event runs its listeners, as the admin create door's account does. It needs the catalogue database (`MYSQL_*`) and refuses without one, naming it: without a database the directory is each process's own memory, and an account written from the console would end with the command instead of reaching the server.

This release amends the example's one schema migration — the currency code held unique, a product's currency and category held by foreign keys, the folded username in a column of its own, the identifier sequence and the session index — so a volume built by an earlier checkout is refused by its fingerprint, by name, until `example:db:reset --force` rebuilds it.

---

## Structure overview

The example lives entirely under the [`./.example/`](./) directory and follows a **flat layout**: each concern lives in its own top-level package, with no `domain/` / `infra/` umbrella layers. All paths below are **relative to `.example/`**.

```
.example/
├── assets/           # frontend sources (TypeScript) and the route manifest they read, built into public/
├── cache/            # cache serializer for the example container
├── cli/              # the application's own CLI commands (app:info, product:list, catalog:report:refresh, messagebus:dispatch, auth:token, internal:sign, totp:code, mailer:send, example:currency:refresh-rates, example:grant:role, example:user:create, example:twofactor:reset, example:exclusive:tick, example:db:reset, example:cache:clear)
├── config/           # application wiring; one file per module hook
├── entity/           # domain entities (Category, Currency, Product, User)
├── event/            # domain event types
├── generated/        # generated service wiring
├── generated_conf/   # generated deployment files (the crontab)
├── handler/          # HTTP handlers (pages + JSON APIs), with category/, currency/, product/, user/ subpackages
├── journal/          # the logger a handler or service writes to, the request's when there is one
├── message/          # message-bus messages (notifications, the outbox notice, the welcome email)
├── messagehandler/   # message-bus handlers
├── migration/        # the schema migrations
├── page/             # HTML page templates
├── persistence/      # the catalogue and archive storages the repositories write through
├── presenter/        # HTTP error / response presenters
├── reporting/        # the catalogue reading and its export
├── repository/       # repository interfaces, their in-memory implementations and the bun-backed ones the shipped .env selects
├── route/            # named route constants and patterns
├── security/         # session auth wiring (login/logout handlers, entry point, token resolver, password hasher)
├── service/          # application services (CategoryService, CurrencyService, ProductService, UserService)
├── subscriber/       # event subscribers
├── twofactor/        # the TOTP enrollment store, encrypted at rest
├── url/              # route registry adapters: the route manifest every page is given
├── public/           # static assets (CSS / JS)
├── var/              # runtime cache and logs
├── embedded_*.go     # build-tag–controlled embedding for env and static assets
├── main.go           # application entry point
├── go.mod / go.sum   # standalone module manifest
├── .env              # example env defaults
└── .gitignore
```

### [`config/`](./config/) — application wiring

The [`config/`](./config/) package keeps [`main.go`](./main.go) small by grouping all setup and integration logic in a single place, with each module hook in its own file:

- [`configure.go`](./config/configure.go) — entry point invoked by `main.go`: registers the example module and the integration module facades (observability, otlp, encrypt, outbox, migrate, cron, websocket, awss3, rueidis); otlp, outbox, awss3 and rueidis are gated on the configuration that enables them, the others are registered unconditionally
- [`module.go`](./config/module.go) — `Module` struct + `Name()` + `Description()` + interface assertions for the module hooks the example implements
- [`security.go`](./config/security.go) — `RegisterSecurity`: access-control rules, role hierarchy, decision manager, firewall
- [`http.go`](./config/http.go) — `RegisterHttpRoutes`: named-route registration for pages and JSON APIs
- [`cli.go`](./config/cli.go) — `RegisterCliCommands`: the application's own CLI commands; `melody:cron:generate` comes from the cron module registered in [`configure.go`](./config/configure.go)
- [`event.go`](./config/event.go) — `RegisterEventSubscribers`: wires the example's domain event subscribers
- [`parameter.go`](./config/parameter.go) — `RegisterParameters`: registers `melody.cron.*` parameters from `APP_CRON_*` env vars plus the example's own `app.*` parameters
- [`service.go`](./config/service.go) — `RegisterServices`: container wiring for repositories, services, and the cache serializer
- [`middleware.go`](./config/middleware.go) — example-specific HTTP middleware (`NewTimingMiddleware`)

### Cron integration

The example demonstrates Melody's [`integrations/cron`](../../integrations/cron/v3/) package. Commands stay plain Melody CLI commands — there is no `cron.Metadata` interface to implement. Schedules are declared separately in [`config/cron.go`](./config/cron.go) through a `cron.Configuration` registry:

```go
cronConfiguration := cron.NewConfiguration().
    InTimezone("Europe/Bucharest").
    Schedule(cron.CommandName(cli.NewCatalogReportRefreshCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "0", Hour: "*"},
        User:     productUser,
    }).
    Schedule(cron.CommandName(cli.NewProductListCommand), &cron.EntryConfig{
        Schedule:  &cron.Schedule{Minute: "0", Hour: "*/6"},
        User:      productUser,
        Arguments: []string{"--limit=2"},
    }).
    Schedule(cron.CommandName(cli.NewCurrencyRefreshRatesCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "*/30", Hour: "*"},
        User:     productUser,
        Timeout:  5 * time.Minute,
    }).
    Schedule(cron.CommandName(cli.NewAppInfoCommand), &cron.EntryConfig{
        Schedule: &cron.Schedule{Minute: "*", Hour: "*"},
    })
```

The schedules are evaluated in `Europe/Bucharest`, the catalogue's business day, so the hourly reading fires on the Bucharest hour whatever zone the process runs in; the in-process runner stamps the minute it evaluates in that zone, and the generated manifests, run by a scheduler that owns its zone, carry nothing for it. The rates refresh is bounded at five minutes under the in-process runner, so a provider that hangs cannot hold the run into the next half hour. `app:info` runs every minute as the worker's heartbeat: every evaluated minute dispatches at least one run, whose output record the journal keeps under the run's id. That heartbeat belongs to the in-process runner. The same `Configuration` also feeds the generated crontab, where the entry appends about 600 bytes a run, some 0.86 MB a day, to `<logs-dir>/app-info.log`, and nothing rotates that file. A deployment of the manifest has its own heartbeat, the `heartbeat.crontab` touch described below, so drop the `app:info` line from the installed crontab or put its log under logrotate. `LogDisabled` is no remedy: the manifest sets no `MAILTO`, so cron would mail the output every minute instead.

`cron.CommandName` is a generic helper that instantiates a constructor and returns the command name, so the schedule references commands by constructor instead of hardcoded strings.

Cron defaults (user, logs directory, destination file, template, heartbeat) come from the parameter system in [`config/parameter.go`](./config/parameter.go). The user is sourced from `APP_CRON_USER`, and the heartbeat is enabled via the `APP_CRON_HEARTBEAT_AUTO_ENABLED` opt-in (which auto-derives `<logs-dir>/heartbeat.crontab` from `melody.cron.logs_dir`) — both env vars live in [`.env`](./.env). [`config/cron.go`](./config/cron.go) reads `app.cron.product_user` (backed by `APP_CRON_PRODUCT_USER`) at registration time and applies it as the per-command user on the `catalog:report:refresh` and `product:list` schedules, demonstrating how the parameter cascade feeds custom values into `cron.Configuration` entries. The third entry, `app:info`, declares no user and falls back to `melody.cron.user`.

### [`main.go`](./main.go) (why it stays small)

`main.go` intentionally contains minimal logic.

It only:

- constructs the Melody application using:
    - `embeddedEnvFiles` (from `embedded_env_*`)
    - `embeddedPublicFiles` (from `embedded_static_*`)
- calls `config.Configure(ctx, app)`
- boots the application and arms the parallel teardown
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

The application also answers `GET /health` without a session, which is the route a monitoring system or a container orchestrator probes. It is public on purpose, so a probe that had to authenticate is not answered with a redirect to the login page instead of the readiness of the process. The other public rules of [`config/security.go`](./config/security.go) are the login and logout doors, the frontend bundle (`/`, `/index.html`, `/assets`, `/favicon`, `/routes`), the greeting under the locales it is served in (`/en/i18n`, `/ro/i18n`, `/ro-RO/i18n`), `/openapi.json` and the cipher round-trip probe, which reads nothing from the caller. A door that is one path is opened by an exact rule, which speaks for that spelling and nothing beneath or beside it: `/health` is public and `/healthz` is not. `/metrics` answers only the scraper presenting `Authorization: Bearer <APP_METRICS_TOKEN>`, which the development prometheus carries; an empty token leaves it public. Every other route carries a role — a door that writes through the example into a backend (the object storage, the outbox, the message bus) carries the write role the catalogue writes carry, and what no rule names falls under the `^/` catch-all, which requires a signed-in user.

The serving settings the [`.env`](./.env) carries:

- a signed-in session is kept in `var/session/session.json` (`APP_SESSION_FILE`), so a browser stays signed in across the restart the development supervisor does on every saved change, and it expires a day after the last request that wrote to it, a sign-in or a change of what it holds, since a request that only reads it does not extend it (`MELODY_HTTP_SESSION_TTL=24h`). One account holds at most five live sessions: a session that ended elsewhere — expired, or cleared because the account changed — has its row dropped by the next sign-in before it counts, and without a database the index is kept in memory and counts again from zero at each restart, while the sessions the file keeps stay valid; the sign-in past them ends the account's oldest, through the session index the schema holds, since the file storage rewrites every live session on each save and the number it pays for would otherwise grow with every sign-in an address can make; a sign-out frees its place;
- a request body past 64 KiB is refused with 413 before a door reads it (`MELODY_HTTP_MAX_REQUEST_BODY_BYTES`), the object storage upload included;
- the bundle's assets are cached for an hour and revalidated by ETag and Last-Modified (`MELODY_STATIC_ENABLE_CACHE`, `MELODY_STATIC_CACHE_MAX_AGE`); a developer who wants every save fetched afresh turns the cache off in `.env.dev.local`, which wins over `.env`;
- the prefixes of the application's own doors, one per first segment of the route table and one per locale of the localized greeting, are never served from `public/` (`MELODY_STATIC_EXCLUDED_PATHS`): the file server answers ahead of routing and the router takes a door with or without its trailing slash, so a file dropped there would otherwise be served to a signed-in caller in place of the door; a door added under a new first segment needs its entry, and the stack checks hold the list against `debug:router`;
- the sign-in submit spends the same per-address write budget as the catalogue's writes, thirty a minute, counted in redis and, without redis, in the process itself;
- the listings travel gzip-compressed to a client that accepts it;
- behind a proxy that terminates tls, the scheme it forwards (`X-Forwarded-Proto`) is believed from the private ranges and loopback, so the session cookie is marked `Secure`; the rate limit's client address, which decides whose budget a request spends, is believed from the balancer alone (`APP_TRUSTED_PROXY_LIST`).

The greeting is served at `/:_locale/i18n/greeting/` for `en`, `ro` and `ro-RO`, the last answered from the `ro` catalogue; another locale matches no route. Its `?count=` drives the cart line through the ICU plural of each catalogue: English has `one` and `other`, and Romanian reads `few` apart from `other` — `2 produse`, `20 de produse`, `101 produse`. The catalogues stay in Go: `JsonDirectoryLoader` reads a directory on disk, and this example also ships as a single embedded binary. The `/internal` firewall carries its own authorization and reads nothing of the global rules, and it refuses an envelope whose expiry sits more than five minutes ahead; `internal:sign --ttl` mints one for a chosen lifetime, uncapped, so an operator can see that refusal.

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

The example schedules four commands in [`config/cron.go`](./config/cron.go) (`catalog:report:refresh` hourly, `product:list` every 6 hours with `--limit=2`, `example:currency:refresh-rates` on the half hour, `app:info` every minute as the heartbeat) plus a heartbeat enabled via `APP_CRON_HEARTBEAT_AUTO_ENABLED=true` in [`.env`](./.env) (the path is auto-derived from `melody.cron.logs_dir`), so the generated crontab is not empty.

The same `cron.Configuration` also drives an **in-process scheduler** for single-binary deployments with no external crontab. `melody:cron:run` ticks in-process and invokes each scheduled command when it is due; `--once` evaluates every schedule against the current time, runs the due commands and exits:

```bash
cd v3/.example
go run . melody:cron:run --once      # kick whatever is due now, then exit
go run . melody:cron:run             # run the scheduler loop until interrupted
```

The runner dispatches each scheduled command with its declared flags, so declared defaults are honored on a scheduled tick exactly as under the cli entry point: `product:list` declares `--limit` with a default of `5` and prints the value it read: `product list: limit=5` when invoked directly, `product list: limit=2` under the runner, which hands it the arguments its entry declares.

`example:grant:role` shows that an application command may declare its own `--role` flag: the runtime's `--role`/`--mode` are recognized only before the command name, so the command receives its flag intact. The flag is required, so the framework refuses an invocation without it before the command runs, naming the flag, and `-r` is its short spelling; the account is the command's one argument. It also holds the example's user service through a `container.Lazy` handle built at command-registration time — the service is resolved at the command's first run, not during the boot phase. The flag is trimmed and has to name one of the three roles the application knows (`ROLE_USER`, `ROLE_EDITOR`, `ROLE_ADMIN`) — the voter compares a role's spelling exactly, so any other spelling would be stored and grant nothing — and the grant goes through the repository's atomic door, which reads and widens the account's set under one lock, so a grant that runs beside an admin update of the same account cannot lose the other's write; an account that already holds the role is a no-op, not a second entry:

```bash
go run . example:grant:role --role ROLE_ADMIN ada    # the command's own --role, required, -r for short; the user is the argument
go run . --role worker app:info                        # the runtime process role
```

`example:user:create` creates the account named as its argument, holding the `--role` it is given (required, one of the three roles, `-r` for short, as `example:grant:role` spells it); the password is the first line of standard input, and a second argument is refused before anything is written. `example:twofactor:reset <username>` removes an account's second factor, secret and recovery codes together: the enrollment door replaces a factor only for a request that presents a current one, so an account that lost its authenticator and spent its last recovery code comes back through an operator, signs in on its password and enrolls again.

`auth:token` mints the HS256 token the `/secure` api reads, signed with `APP_JWT_SECRET`: `--role` writes the roles claim (`ROLE_USER` when neither flag is given), and `--scope-role` writes the grant into the `scope` claim the way an OAuth-style issuer carries it, read by the firewall's scope enricher; a token minted with `--scope-role` alone carries an empty roles claim, so the role it holds on the route is the scope's.

The application declares its own version to every command document and to `debug:version` through `output.SetApplicationVersion`, from `main.applicationVersion`, which a build sets with `go build -ldflags "-X main.applicationVersion=<version>"`; a build that sets nothing reports `dev`.

A console run has no signed-in user, so the audit trail and the catalogue journal name the run itself as the actor of the grant, `process:<id>`, the process id every journal record of that run carries; a signed-in request names its user, and an unauthenticated one the system.

---

## Platform integrations (optional, env-gated)

The example wires **every v3 platform integration**. Each backend that needs external infrastructure is **gated on an environment variable**: when the variable is unset the application boots with an in-process fallback (remove the integration lines from [`.env`](./.env) and the example runs with zero infrastructure), and when it is set the matching integration is activated and resolved through the same core service constant, so the rest of the app is unchanged. The committed `.env` sets these variables to the dev compose service endpoints so `./dc up:all` wires everything out of the box.

| Integration                                                                                                                                      | Activated by                                 | Falls back to            | Reached at                                    |
|--------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------|--------------------------|-----------------------------------------------|
| [`opentelemetry`](../../integrations/opentelemetry/v3/) — Prometheus metrics middleware and lifecycle handler decorator                          | always on                                    | —                        | `GET /metrics`                                |
| [`websocket`](../../integrations/websocket/v3/) — WebSocket bound to the SSE hub                                                                 | always on                                    | —                        | `GET /ws` (and `GET /events/stream` for SSE)  |
| `encrypt` ([`bunorm/v3/encrypt`](../../integrations/bunorm/v3/encrypt/)) — AES-256-GCM cipher                                                    | always on                                    | —                        | `GET /encrypt/roundtrip`                      |
| [`amqp`](../../integrations/amqp/v3/) — durable message-bus transport                                                                            | `AMQP_DSN`                                   | in-memory transport      | `POST /messagebus/dispatch`                   |
| [`awss3`](../../integrations/awss3/v3/) — S3 object storage (`storage.ServiceStorage`)                                                           | `S3_ENDPOINT`                                | local filesystem storage | `GET /platform/check`                         |
| [`rueidis`](../../integrations/rueidis/v3/) — Redis cache backend, distributed lock (`lock.ServiceLocker`), revocable token store, SSE backplane | `REDIS_ADDRESS`                              | in-memory cache/lock     | `POST /access-token/issue/`                   |
| [`bunorm/mysql`](../../integrations/bunorm/mysql/v3/) — MySQL `GET_LOCK` distributed lock                                                        | `MYSQL_HOST` (when `REDIS_ADDRESS` is unset) | in-memory lock           | `GET /platform/check`                         |
| [`bunorm`](../../integrations/bunorm/v3/) — bun ORM `*bun.DB`, transparent column encryption, field-level audit trail                            | `MYSQL_HOST`                                 | —                        | every catalogue write: audited and journalled |
| [`bunorm/migrate`](../../integrations/bunorm/migrate/v3/) — the `db:*` migration command family                                                   | always on (fails at `Run` without a database) | —                        | `db:migrate`, `db:status`, `db:rollback`      |
| [`bunorm/pgsql`](../../integrations/bunorm/pgsql/v3/) — the reading archive's PostgreSQL database and its advisory lock                           | `PGSQL_HOST`                                  | in-memory archive/lock   | `GET /reports/api/history/`                   |

`GET /metrics` carries two request families built on one meter. The metrics middleware counts the requests the pipeline handles, labelled by route (`http_server_request_count_total`); the handler decorator of [`opentelemetry.NewHandlerDecorator`](../../integrations/opentelemetry/v3/handler_decorator.go), which wraps the whole kernel handler, counts every request by method and status (`http_server_lifecycle_request_count_total`), the ones the firewall refuses before any middleware runs included. A 401 the firewall answers therefore shows in the lifecycle family only. The decorator's tracer is a no-op: the spans are the otlp module's tracing middleware's, and the decorator keeps the caller's trace context so that span stays under it.

The lock service follows a single priority: Redis if configured, otherwise MySQL, otherwise in-memory. Transparent encryption-at-rest is not shown through a route of its own: the two-factor enrollment table stores the shared secret and the recovery codes as `encrypt.EncryptedString` columns, so `POST /twofactor/enroll` writes ciphertext MySQL holds and `POST /twofactor/verify` reads it back to recompute a code. Both act on the caller's own account — the identifier comes from the authenticated token rather than from the request. Enrolling again replaces the enrollment that is there, so it asks for a factor of the one it replaces: a TOTP code on `X-2FA-Code` or a recovery code on `X-2FA-Recovery-Code`, spent as the verification door spends them; on the session alone it answers `401` with `{"factor":"totp"}`, since a stolen session must not become the thief's second factor. An account that lost both its authenticator and its codes is reset from the console (`example:twofactor:reset`). The enrollment goes with the account: the schema cascades the row on the deletion of its user, the authoritative release, and a subscriber also deletes it on the deletion event, journaling a store it cannot reach rather than failing the deletion. The account's tokens go first: the token release outranks every other listener of the deletion — the dispatch stops at the first listener that fails, the release included, so no outage of theirs can skip it; an outage of the token store itself fails the deletion with the account already gone, and the device firewall refuses the tokens it left — and it both deletes the device tokens the store holds and publishes an account-wide revocation at the deletion's instant, which refuses the bearer tokens no store holds. A device token issued while its account was being deleted is taken back by the issue door, which reads the account again after storing it, and the device firewall honours a token only while the account it names exists, with that account's current roles. `GET /encrypt/roundtrip` reports the ciphertext beside the value that came back, which is the half a stored column cannot show.

Several wirings deliberately defer their resolution to first use instead of the composition root:

- `example:exclusive:tick` is wrapped in `lock.NewExclusiveCommand` over `lock.NewLazyLocker`, which resolves the registered locker at the first `CreateLock` — with a distributed locker configured (Redis or MySQL), run it from two shells at once and exactly one executes while the other exits zero. Under the in-memory fallback the exclusivity is per-process, so two separate shells both execute.
- The in-process cache fallback is per process too, and that bounds what a console writer can promise: the entities are cached for at most ten minutes and cleared by name by listeners subscribed to the write events, which run in the process that DISPATCHED. With Redis the cache is shared and `example:grant:role`, `example:currency:refresh-rates`, `example:db:reset` or `example:cache:clear` reach the running server; without it they reach their own process, and a server started beside them keeps what it cached until it restarts — each of the four says so on its output when that is the wiring it ran under. `example:cache:clear` is the door that empties this application's namespace and nothing else: a row changed through no door of this application — edited by hand, restored from a backup — dispatches no write event, so its cached entity is served until something clears it, and the command does that without the reset's two databases.
- The cache keys carry, inside the `melody-example-v3:cache:` namespace, a token computed from the layout of the cached types ([`cache.LayoutToken`](./cache/gob_serializer.go)): gob decodes by field name and stays silent about a field the payload does not carry, so a build that added a field over a live Redis read every entry with that field at zero — a currency with no rate, refused by every conversion — until something dropped the keys. Under the token a build reads only what a build of the same layout wrote. What an older build left stands orphaned in Redis, outside the reach of `example:db:reset` and `example:cache:clear` (which clear the current layout's namespace); after a deploy that changed a cached type, drop the old prefix once — `redis-cli --scan --pattern 'melody-example-v3:cache:*'` lists both.
- The transactional-outbox module ([`config/outbox.go`](./config/outbox.go)) is registered in the `StoreFactory`/`RelayFactory` shape: the store (which ensures the `melody_outbox` schema) and the relay (which opens the transport) are built from the container at first use, and the module contributes the `melody:outbox:relay` command over the same lazily-resolved relay. Endpoints: `POST /outbox/enqueue`, `POST /outbox/relay`, `GET /outbox/status`.
- The encrypt module resolves the shared `*bun.DB` through a `DatabaseFactory` evaluated at the first `melody:encrypt:database` run, so http- and worker-mode processes register the command without touching the database. The cipher encrypts under `APP_ENCRYPT_CURRENT_KEY` and decrypts under every key `APP_ENCRYPT_KEYS` lists (`id:key`, comma separated, secret-marked; without a list, the development key — in development only, see [Before production](#before-production)): a key is retired by listing the next, making it current, running `melody:encrypt:database --mode reencrypt --target-key <next>` over the encrypted columns and only then removing the old one. A malformed list stops the boot naming the entry's position — and its id only when the entry was written `id:key`, since an entry without the separator may be the key itself — never the key.
- The outbox relay drains under the application's locker (`melody-example-v3:outbox:relay`), so replicas running `melody:outbox:relay` from cron drain from one at a time; the batch claim alone already keeps two from publishing one row.
- The internal firewall remembers the nonces of accepted envelopes in Redis under `melody-example-v3:nonce` when Redis is wired, so an envelope accepted by one replica is refused by every other; without Redis it keeps its in-process guard.
- Deleting an account releases its device tokens: a listener on the deletion event deletes every token the account holds from the token store, so a deleted account keeps no api access until its tokens expire.
- A product read counts itself on the cache backend's own increment (`example-product-views-<id>`) and answers `views`; the counter is a hint rather than a ledger — a cache failure leaves the key out of the answer and is journaled, and emptying the cache namespace resets it.
- `GET /storage/object/link?key=&ttl=` answers a presigned download link to a stored object, so a client reads the bytes straight from the object store; the link lives `ttl` seconds (five minutes by default, an hour at most), and an absent object is a `404`.
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
- Every rate of the catalogue is quoted against ONE base, named by `RATES_BASE_CURRENCY` (`EUR` by default,
  the seed's base): a conversion cancels the base by dividing one rate by another, which is arithmetic only
  while every rate shares it. The refresh therefore judges the provider's document whole before it writes a
  quote, and refuses it — nothing written, exit non-zero, both bases named — when it is quoted against another
  base, carries no `asOf`, is stamped after the provider answered with it, or quotes one currency under
  several spellings, every spelling named. Codes are matched folded, so a provider writing `usd` quotes the seed's `USD`; two spellings
  means two keys that fold onto one code — a key repeated letter for letter is collapsed by the JSON decoder
  before the refresh sees it, the last value winning, which is the decoder's rule and not the refresh's. A
  base that is configured EMPTY (`RATES_BASE_CURRENCY=` present in `.env`, where an absent key falls back to
  `EUR`) refuses every document. The instant is held at the microsecond the column holds.
- A reading carries its instant in two reference frames. `asOf` is stamped on the PROVIDER's clock, and the
  catalogue orders readings on THIS one: a provider whose clock ran ahead and was then set back would otherwise
  have every honest reading after the correction judged older than the one stamped before it. The answer's
  `Date` — plus the `Age` a cache in front of the provider adds — is the provider's clock at the moment it
  answered, so the refresh measures the offset between the two clocks on every answer, to within the second the
  date is read to (two with an `Age`) and half the round trip, and stores the reading twice: `provider_rate_as_of`, the stamp as it
  came, which names the reading, and `rate_as_of`, the same instant moved onto this clock, on which every order
  is judged. A clock set back moves the stamp and the answer together, so the reading after it is newer here; a
  replay keeps an old stamp under an answer given now, so it is older here — when the replay carries a current `Date`. The offset is trusted within five
  minutes, in either direction: from one answer, a provider whose clock runs late cannot be told from a verbatim
  replay of an old answer that kept its `Date` and dropped its `Age`, which an offset of any size would lift over
  the newer reading it replays, so an answer measured further off is refused, naming the offset, the `Date` and
  the `Age`. The last column of the table, `PROVIDER_CLOCK`, prints the offset measured (`+0s` for a provider
  that agrees with this clock to within what one answer can tell), or `unmeasured` for an answer without a
  readable date or with an `Age` at or above the largest a cache may send — its stamps are then taken as they came, no
  later than the moment the answer arrived, and one more than five minutes ahead of this clock is refused. What
  the five minutes admit is stated, not hidden: a replay that kept its `Date`, or a dateless replay of a reading
  stamped ahead of its arrival, cannot be told from a provider whose clock moved, and is written over a newer
  reading; it is at most five minutes old, and the next honest reading replaces it.
- What the refresh did is printed under one heading per currency: `UPDATED` (written), `SKIPPED` (not quoted
  by the provider, or deleted inside the run), `UNCHANGED` (the reading the catalogue already held — the
  same provider stamp at the same rate, however this clock measured it on arrival — nothing written, no event, the
  currency's cache entries dropped where they are not the row), `STALE`
  (older than the reading stored — a replay, kept out) and `REFUSED` (a quote the write door would not take:
  zero, negative, infinite or outside `[1e-6, 1e9]`). One refused quote does not stop the sweep: the currencies
  after it are written on the same run, and the exit code names the currencies refused. A backend failure does stop
  it, and the line names what the door did before it failed: a quote written whose listeners were not told is
  counted `UPDATED` and said to be written, with its cache entries standing until the next tick; an unchanged
  quote whose cache drop failed is counted `UNCHANGED` and the drop is named — neither reads as a quote that
  could not be written.
- `RATES_API_KEY` is the provider's api key, sent as `x-api-key` on every rates request; the development provider refuses a request without it, and the key is marked secret, so `debug:parameters` redacts it. The client journals no request header.
- `GET /products/api/read/:id/?currency=USD` answers the product with its price restated in the currency the
  caller named, stamped with the instant of the quote it used. Without the parameter the answer carries no
  conversion at all; a code the catalogue does not carry is a `400`, while a product quoted in a currency the
  catalogue lost, or against a rate that is not a usable price, is the catalogue's fault and a `500` with the
  cause journaled.
- The nomenclature is written through three doors that require `ROLE_EDITOR`, as the product writes do, while
  the listing `GET /currencies/api/read/` keeps `ROLE_USER`: `POST /currencies/api/create/` takes an `id`
  (minted when absent), a three-letter upper-case `code` — judged by the application's own `currencyCode` rule,
  registered on the validator the container serves ([`validation/currency_code.go`](./validation/currency_code.go)),
  on the create and on the update — a `name` and the first `rate`, stamped with the
  instant of the create and refused with a `400` when it is not a usable price, while an `id` another currency
  holds is a `409`, as it is on the product create, and so is a `code` another currency holds — the conversion
  finds a currency by its code, so the code is the currency's identity and the schema holds it unique;
  `PUT /currencies/api/update/:id/` renames the code and the name and never writes the rate, which only the
  refresh quotes, and answers the same `409` for a code another currency holds; `DELETE
  /currencies/api/delete/:id/` removes the row, and answers `409` while a product is priced in it — the product
  table's foreign key holds that on the database, and without one a catalogue lock holds the check and the delete
  together against a concurrent product write, so the conversion of a product never loses its currency to a
  delete. A missing currency is a `404` on both. A product write naming a category or a currency that does not
  exist is the caller's `400`. The currencies are not audited. The write doors validate the body in the
  spelling they store, trimmed: a name of one character padded with a space is refused, not stored.
- `catalog:report:refresh` pushes its reading to `APP_REPORTING_EXPORT_ENDPOINT` when one is configured, and
  takes the command's exit code with it if the sink refuses.

The two endpoints are the two SHAPES the client supports, and they are not interchangeable. `RATES_BASE_URL`
is a **base** and carries its trailing slash: the client resolves every target against it by RFC 3986, so the
slash is what keeps `/v1` a prefix instead of a segment the merge cuts, and a base written without one is
refused when the client is built, at its first resolution, since its provider runs lazily. The shipped value is plain `http://` only because it names the
stack's own balancer, which answers a fixed document inside the compose network; point it at a real provider
over `https://`, since whoever can rewrite the provider's answer sets every price the catalogue converts. A
based client also refuses a target that resolves off its origin,
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

The schema is owned by one migration set in [`migration/`](./migration/) — a single MySQL DDL migration holding the eight tables this example owns and the table it records its own fingerprint in, every constraint declared with its table: the four catalogue tables, the session index, the identifier sequence, the journal and the two-factor enrollment table neither frozen major carries. The constraints are the unique key on the currency code, which the conversion finds a currency by; the two foreign keys a product holds on its currency and its category, which refuse a delete from under it and a reference that names nothing; and the unique key on `username_normalized`, the name as the application folds it in Go and writes beside the name as spelt — the identity the cache keys and the listeners share, which MySQL's `LOWER()` does not reproduce for every script — which is what holds a name against two callers that pass the repository's read-then-write check at the same moment. The identifier sequence keeps, per prefix, the highest identifier ever stored: a create mints past it, so a deleted entity's identifier is never handed to the next one, with the history and the references that still name it. The set is one migration because this application has no history — an example has one state, the present one, so its schema is the statement of that state rather than the record of how it got there, and a database left in an older shape is answered by `example:db:reset` rather than by a step that repairs its past. Until it is, it is refused by name: the set is recorded as applied by name and its tables are created `IF NOT EXISTS`, so it passes such a volume untouched, and the first resolution compares every table the set's own statements create with the table the volume holds — a missing column or one no statement declares refuses the volume with the table, the columns and `run example:db:reset --force`, where the first request reading the column used to answer 500. The columns alone do not show a type, a collation, a key or a constraint changed under the same names, so the set also records, in `melody_example_v3_schema_fingerprint`, a hash of every statement it built the volume with, and the first resolution refuses a volume whose hash is another — or that holds none, having been built before the set recorded one — the same way. A fingerprint vouches only for tables the set built: a volume that holds the set's tables without recording the set — provisioned before it, or with its bookkeeping lost — would have every `CREATE ... IF NOT EXISTS` pass over it and the hash written over tables built from anything, so the set refuses before its first statement, naming the tables it found and `example:db:reset --force`. What tells that volume from one the set itself began and did not finish is the set's own row: it is written as `building` before the first statement and sealed as `built` after the last, so a run interrupted halfway — a database that dropped mid-set — is finished by the next run under the same code, while a volume holding the tables with no row of the set's, or a row still building under another code's hash, is refused; the first resolution vouches only for a row sealed `built`. This is the catalogue's set, on the catalogue's connection; the reading archive has a set and a command family of its own, described below. Two doors run the set, so neither can drift from the other:

- the **composition root and the repository constructors** call `migration.EnsureMigrated`. Each repository constructor calls it before seeding, and so does the provider of the two-factor store, at the first request that needs one — which is what keeps a freshly recreated volume usable with no operator step, and what lets a refusal heal: the store used to be built once at boot, so a database briefly down, or a volume a reset had yet to bring here, left `/twofactor/*` unregistered until the process restarted; resolved per request, a refused store answers 503 and the next request asks again, and a user deletion that cannot reach the store journals it and goes on, the cascade on the account having released the row. It is also why every `CREATE TABLE` carries `IF NOT EXISTS`: several processes of the example may apply the set at the same time, serialized by bun's migration lock with a bounded retry that names `db:unlock` when it gives up — the retry also outlives a refusal a retry can heal, a lock wait timeout, a deadlock or a dropped connection, while a refusal no wait heals, a missing grant, is answered at once;
- the **`db:*` command family** (`db:init`, `db:migrate`, `db:rollback`, `db:status`, `db:unlock`, `db:create`) runs the same set from the operator's side. It comes from the [`integrations/bunorm/migrate`](../../integrations/bunorm/migrate/v3/) module facade registered in [`config/configure.go`](./config/configure.go), pinned to the example's own manager registry service.

`example:db:reset` is the third door, and the only one that goes backwards. It drops the tables the set owns, drops and recreates the bun bookkeeping with them, applies the schema again and reseeds every nomenclature in one pass. The audit trail is emptied and then receives the reseed as ONE audit transaction: the products and users seeded, each with its insert entry, under a transaction row naming the run (`process:<id>`) and the command. It exists because this application has no history: a database left in an older shape — carrying bookkeeping rows that name migrations this schema no longer has, or identifier columns created under the collation the tables had before they compared identifiers byte for byte, or a two-factor table created before its rows were tied to their account by a foreign key — is brought to the present state here, by a command an operator runs deliberately, rather than by code every process pays for at boot. The accounts go with their table through no door that publishes a deletion, and the reseed hands their identifiers out again, so with Redis the reset also empties the token store's namespace — every device token and revocation epoch — right after the drop; without Redis the device tokens live in the running server's own memory, out of a console command's reach, and the plan says so. It refuses to act without `--force`, printing what it would drop and exiting zero, and it is a command of the application rather than of the migration module: dropping an application's whole schema is not an operator door a published module should grow. The audit trail is emptied with the rest, though its table is not dropped: the schema belongs to the module that opens it, the rows belong to this application, and a trail carried across a reset would name entities that no longer exist, over identifiers the reset hands out again with the sequence it dropped.

The connection itself is declared in a `bunorm.ManagerRegistry` ([`config/database.go`](./config/database.go)) rather than opened directly: the registry is the one door the commands resolve, and the `*bun.DB` the rest of the application holds is its default manager. Closing the registry is what closes the pool — and what puts it in reach of the ordered teardown is that the catalogue storage RESOLVES the handle rather than holding the one the composition root built, because the container records a dependency at the moment one provider resolves another. That same resolution is what installs the application's journal on the registry, so the pool's reporting and bun's diagnostics leave the emergency logger at the first repository resolution instead of never.

The module is registered whether or not a database is configured, so the command surface does not change between environments; without one every `db:*` command fails at `Run` with the container refusal naming the registry service.

The framework's own tables are not in the set: the outbox store and the audit registry each open their schema through the module that owns it, and a set belonging to the application would claim tables it does not own.

The set lives in a database of this major's own — `melody_example_v3`, named in [`.env`](./.env). The three example applications share the development MySQL and not a database in it: the bun bookkeeping tables (`bun_migrations`, `bun_migration_locks`) keep their default names, and bun matches an applied migration by name, so on one shared database the three sets would share one bookkeeping table and the first to land would answer for the others.

### The reading archive — the second database

This example holds **two** databases: the catalogue on MySQL and an archive of catalogue readings on PostgreSQL. They are two independent switches — `MYSQL_HOST` and `PGSQL_HOST`, each empty-means-unwired — so all four combinations boot: both live, either one alone, or neither.

The archive is what the scheduled report leaves behind. `catalog:report:refresh` takes a reading of the catalogue, leaves it in the cache, records it — one row per reading, so `GET /reports/api/history/` can answer how the catalogue has moved — and then pushes it to the export endpoint: the archive is the durable half and depends on nothing the sink does, so a sink that refuses takes the exit code after the row is there rather than leaving a hole in the history. The archive is opened when the refresh acquires its archive lock, whose resolution opens the PostgreSQL handle, or when the history door resolves its repository — never at boot: a process that takes no reading never dials PostgreSQL, and the open is bounded by a budget of its own, three attempts inside a second, under the process's signal context. The listing requires `ROLE_USER`, the role the category and currency listings carry — a reading is the catalogue counted, so whoever may not read the nomenclature may not read its history either; the detailed product listing requires `ROLE_EDITOR` — and takes a `limit` the door caps, because the archive grows for the life of a volume.

Three things about it are worth reading rather than inferring:

- **A reading's identity is the instant it was taken at, to the second.** That is the resolution the reading already states about itself: its payload writes `recorded_at` as RFC3339, which carries no fraction, so a key kept finer would disagree with the very value it keys. The instant is the table's primary key, so two refreshes inside one second are the same reading — and the second one is told it did not write rather than being failed.
- **The archive is written by the SCHEDULE, not by a request.** A reading is taken on the request path too, whenever a caller finds a cold cache; archiving there would put a write to a second database on a read, make a read door fail when PostgreSQL is down, and fill the archive with rows nobody scheduled.
- **The write is taken under a PostgreSQL advisory lock** ([`pgsql.NewLocker`](../../integrations/bunorm/pgsql/v3/lock.go)), registered under a name of its own rather than the framework's locker service — that one is Redis when Redis is configured, and the archive is not on Redis. A lock held in the very database being written is exclusion that cannot disagree with the write it guards, and a session advisory lock is released when its connection drops, so a process that dies mid-refresh leaves nothing to clean up. The lock is taken around the whole run — the reading, the row, the export — so processes that OVERLAP on one schedule record one reading between them: the loser takes no reading at all and says so. Two runs that do not overlap, one host's tick a second after another's, are two readings keyed on the instant each took; the identity of a reading is the second it was taken at, and the lock does not change that. Without PostgreSQL the locker under that name is the in-process one, over the in-process archive — which is a repository of ONE process: the refresh command records into its own process's archive and exits, and the http server's history door reads its own, which nothing writes, so without PostgreSQL the history door answers an empty list. The in-process archive exists so the command has a producer to run against, not so the server has something to show; the same topology holds for the cache's in-process fallback, described above.

The archive's schema is a set of its own, in the same package, and it has to be: `bun_migrations` is per database, so two databases need two sets and one set could never span them. It is exposed as a migration **context** of the `bunorm/migrate` module, which gives it the `db:archive:*` command family (`db:archive:migrate`, `db:archive:status`, `db:archive:rollback`, `db:archive:unlock`, …) pinned to its own manager — so `db:archive:migrate` defaults to PostgreSQL, and only an explicit `--manager` sends it elsewhere. The base `db:*` family is pinned to the catalogue's manager for the same reason, and that pin is not symmetry: unpinned it takes the registry's default, and in an environment that wired the archive alone an unqualified `db:migrate` would aim the catalogue's MySQL DDL at PostgreSQL.

`GET /reports/api/export/?limit=` answers the same readings as a `text/csv` attachment named `catalog-readings-<date>.csv`, for a spreadsheet; a cell that would read as a formula (`=`, `+`, `-`, `@`, a tab or a carriage return first) is prefixed with an apostrophe.

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
- **Always `up:all`, not plain `up`.** The backends live on the compose `all` profile, so `./dc up` / `./dc up:minimal` start only the three dev containers (`dev`, `dev-v1`, `dev-v2`) and the load balancer — the example would then have no Redis/MySQL to reach. Use `./dc up:all` whenever you want the live integrations.
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
PGSQL_HOST="localhost" PGSQL_PORT="5433" \
SMTP_ADDRESS="localhost:1026" \
OTEL_EXPORTER_OTLP_ENDPOINT="localhost:4317" \
go run .
```

---

## API response envelope

Most JSON endpoints return a small, consistent response envelope:

- `status`
- optional `data`
- optional `error`

This keeps frontend code predictable and minimizes ad-hoc handling.

A refusal of the server's own class — a `500` a handler answers as a response, with the cause it holds — is
JOURNALED by the presenter, at error, with the cause, the status, the public message and the route as the
router matched it: the kernel journals a handler's failure only when the failure is RETURNED, so a door that
answered it as a response reached the terminate listener alone, one info line and no cause, and outside
development the reason a door answered `500` existed nowhere. A client's own refusal, below `500`, is not
journaled; its cause travels in the body's debug-gated context under the development environment alone.

One account holds at most four streams open at once and the process 256, counted across the event stream and the `/ws` websocket route: past either, the next one is refused with `429` before anything is committed — the websocket route through a middleware in front of the integration's handler, which accepts the upgrade itself — and a stream that closes frees its place.

The event stream (`GET /events/stream/`) re-arms the server's write deadline before every frame and sends a
keepalive comment every half of it, so a stream that outlives the server's `WriteTimeout` keeps delivering —
`net/http` arms that deadline once, from the request line, and the first event published after it used to be
the one lost, on a connection the client still believed open. A write that fails while the client is still
there is journaled as a frame lost; a client that left is the ordinary end of a stream and journals nothing.

A refusal for a client that asks for `text/plain` is written as lines rather than handed to the plain-text serializer, which would print the envelope's fields bare: the status and the public errors first, then the request id, the time and the rest of the context in key order, and under the development environment the cause's trace one frame per line. A success keeps the serializer's rendering.

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

- nothing else — the binary carries the `.env` file and every asset that was in `public/` **at the moment `go build` ran**, which is why the frontend build above has to come first

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
