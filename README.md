<p align="center">
    <img src=".assets/logo.png" alt="Melody" width="128"/>
</p>

# Melody

[![Go >= 1.25](https://img.shields.io/badge/go-%3E%3D1.25-00ADD8)](https://go.dev/) [![Go Report Card](https://goreportcard.com/badge/github.com/precision-soft/melody)](https://goreportcard.com/report/github.com/precision-soft/melody) [![Go Reference](https://pkg.go.dev/badge/github.com/precision-soft/melody.svg)](https://pkg.go.dev/github.com/precision-soft/melody) [![License MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

Melody is a Go framework focused on building **HTTP applications and CLI commands** on top of the same runtime, container, configuration, logging, and validation infrastructure.

> **Using Melody for the first time?** Use **v3**: it is the stable, actively maintained version.
> Its module lives in [`./v3/`](./v3/) and a runnable showcase lives in [`./v3/.example/`](./v3/.example/).
> v1 and v2 are feature-frozen; see [Versions & support](#versions--support).

## Contents

- [Getting started](#getting-started)
- [Why Melody](#why-melody)
- [Architecture](#architecture)
- [Packages](#packages)
- [Extensibility](#extensibility)
- [Integrations](#integrations)
- [Build tags](#build-tags)
- [Example application](#example-application)
- [Versions & support](#versions--support)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [Getting help](#getting-help)
- [License](#license)

## Getting started

Create a module and install Melody v3 (requires Go 1.25 or newer):

```bash
mkdir hello-melody && cd hello-melody
go mod init example.com/hello-melody
go get github.com/precision-soft/melody/v3
```

Create `main.go` with a minimal HTTP application:

```go
package main

import (
    "context"
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/application"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    // "contract" packages hold the interfaces your code is written against.
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func main() {
    app := application.NewApplication(context.Background(), nil, nil)

    app.RegisterHttpRoute(
        "GET",
        "/health",
        func(
            _ melodyruntimecontract.Runtime,
            _ nethttp.ResponseWriter,
            _ melodyhttpcontract.Request,
        ) (melodyhttpcontract.Response, error) {
            return melodyhttp.NewResponse(nethttp.StatusOK, []byte(`{"status":"ok"}`)), nil
        },
    )

    app.Run()
}
```

> **Note:** An HTTP app won't start unless it finds at least one key in a `.env` file. This stops
> you from running with only the built-in defaults by mistake. For local development, one line is enough.

Run it and call the endpoint (the HTTP server listens on `:8080` by default):

```bash
echo 'MELODY_ENV=dev' > .env
go run .
curl http://localhost:8080/health
# {"status":"ok"}
```

### Next steps

- Read the [example application](./v3/.example/README.md): a realistic catalog app that wires modules, services, security, sessions, events, CLI commands, and every platform integration. Run the whole showcase (the example and every backing service) from the repository root:

  ```bash
  ./dc up:all --build
  ```

  Then open http://localhost:8180. The showcase uses port `8180` (`DEV_HTTP_HOST_PORT`) so it doesn't clash with the `:8080` default above. Use `./dc up:all`, not plain `./dc up`, because the backing services are on the compose `all` profile; see [running fully against containers](./v3/.example/README.md#running-fully-against-containers).
- Browse the [v3 package documentation](./v3/.documentation/package/) for the API reference.
- Add an [integration](./integrations/) (database, Redis, message broker, object storage, observability).

## Why Melody

Melody is designed for teams that want:

- A single **service container** and **runtime lifecycle** shared by HTTP and CLI entrypoints.
- Deterministic wiring: behavior is assembled through modules, services, and explicit registration rather than global state.
- Clear boundaries between userland APIs (what you build on) and framework internals (what you do not depend on).

Router-first frameworks such as Gin, Echo, or Chi start from HTTP routing. Melody starts from the
**container and lifecycle**, so HTTP handlers and CLI commands share the same services, configuration, and events.

## Architecture

At a high level, a Melody application is assembled as follows:

- **Application** ([code](./v3/application/)) wires modules and services into a **container** ([code](./v3/container/)).
- A **runtime** ([code](./v3/runtime/)) owns the lifecycle (boot/compile/run/shutdown) and creates request/command scopes.
- The **kernel** ([code](./v3/kernel/)) is the compiled core handed to both HTTP and CLI: it holds the container, the clock, and the event dispatcher, and dispatches the kernel events a request travels through.
- **HTTP** ([code](./v3/http/)) uses the runtime and container scopes to run middleware and dispatch handlers.
- **CLI** ([code](./v3/cli/)) runs commands inside the same runtime and container infrastructure.
- Cross-cutting packages are wired as services: [logging](./v3/logging/), [event](./v3/event/), [validation](./v3/validation/), [cache](./v3/cache/), [session](./v3/session/), [security](./v3/security/).

## Packages

The list below is the **v3** package surface. Each package links to its source folder and its package documentation.
v1 and v2 do not include `lock`, `mailer`, `messagebus`, `openapi`, `storage`, `translation`, or `wiring`.

| Package     | Links                                                                                         | Purpose                                                                                  |
|-------------|-----------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------|
| APPLICATION | [code](./v3/application/) · [docs](./v3/.documentation/package/APPLICATION.md)                | Application bootstrap, module registration, and run modes                                |
| BAG         | [code](./v3/bag/) · [docs](./v3/.documentation/package/BAG.md)                                | Typed value access and conversion rules used by configuration                            |
| CACHE       | [code](./v3/cache/) · [docs](./v3/.documentation/package/CACHE.md)                            | In-process caching contracts and implementations                                         |
| CLI         | [code](./v3/cli/) · [docs](./v3/.documentation/package/CLI.md)                                | CLI contracts, command registration, and execution model                                 |
| CLOCK       | [code](./v3/clock/) · [docs](./v3/.documentation/package/CLOCK.md)                            | Clock abstraction for deterministic time and testing                                     |
| CONFIG      | [code](./v3/config/) · [docs](./v3/.documentation/package/CONFIG.md)                          | Configuration loading and composition                                                    |
| CONTAINER   | [code](./v3/container/) · [docs](./v3/.documentation/package/CONTAINER.md)                    | Dependency injection container, scopes, service factories, and lifecycle                 |
| DEBUG       | [code](./v3/debug/) · [docs](./v3/.documentation/package/DEBUG.md)                            | Built-in CLI debug commands (container, events, router, middleware, parameters, version) |
| EVENT       | [code](./v3/event/) · [docs](./v3/.documentation/package/EVENT.md)                            | Deterministic event dispatching and subscriber/listener contracts                        |
| EXCEPTION   | [code](./v3/exception/) · [docs](./v3/.documentation/package/EXCEPTION.md)                    | Error wrappers, context propagation, and fail-fast helpers                               |
| HTTP        | [code](./v3/http/) · [docs](./v3/.documentation/package/HTTP.md)                              | HTTP server, routing, middleware execution, and request handling                         |
| HTTPCLIENT  | [code](./v3/httpclient/) · [docs](./v3/.documentation/package/HTTPCLIENT.md)                  | Outbound HTTP client contracts and helpers                                               |
| INTERNAL    | [code](./v3/internal/) · [docs](./v3/.documentation/package/INTERNAL.md)                      | Framework-internal helpers; not for userland use, may change without notice              |
| KERNEL      | [code](./v3/kernel/) · [docs](./v3/.documentation/package/KERNEL.md)                          | Connects the application, runtime, and HTTP/CLI wiring                                   |
| LOCK        | [code](./v3/lock/) · [docs](./v3/.documentation/package/LOCK.md)                              | Named distributed-lock contracts with an in-memory locker (Redis/SQL via integrations)   |
| LOGGING     | [code](./v3/logging/) · [docs](./v3/.documentation/package/LOGGING.md)                        | Structured logging contracts and conventions                                             |
| MAILER      | [code](./v3/mailer/) · [docs](./v3/.documentation/package/MAILER.md)                          | Email over a pluggable transport (SMTP, in-memory) with RFC 5322 / MIME rendering        |
| MESSAGEBUS  | [code](./v3/messagebus/) · [docs](./v3/.documentation/package/MESSAGEBUS.md)                  | Transport-agnostic async message bus, middleware stack, routing, and consumer worker     |
| OPENAPI     | [code](./v3/openapi/) · [docs](./v3/.documentation/package/OPENAPI.md)                        | OpenAPI 3.0.3 document generation from routes and Go types                               |
| RUNTIME     | [code](./v3/runtime/) · [docs](./v3/.documentation/package/RUNTIME.md)                        | Runtime lifecycle, boot/compile/run, and wiring orchestration                            |
| SECURITY    | [code](./v3/security/) · [docs](./v3/.documentation/package/SECURITY.md)                      | Access control rules, authentication integration points, and security wiring             |
| SERIALIZER  | [code](./v3/serializer/) · [docs](./v3/.documentation/package/SERIALIZER.md)                  | Serialization contracts and helpers for requests and responses                           |
| SESSION     | [code](./v3/session/) · [docs](./v3/.documentation/package/SESSION.md)                        | Session storage contracts and request/session lifecycle                                  |
| STORAGE     | [code](./v3/storage/) · [docs](./v3/.documentation/package/STORAGE.md)                        | Object-storage contracts with a local filesystem backend (S3 via integrations)           |
| TRANSLATION | [code](./v3/translation/) · [docs](./v3/.documentation/package/TRANSLATION.md)                | Message catalogs with an ICU-subset formatter (placeholders, plural, select)             |
| VALIDATION  | [code](./v3/validation/) · [docs](./v3/.documentation/package/VALIDATION.md)                  | DTO validation engine, constraints, and errors                                           |
| VERSION     | [code](./v3/version/) · [docs](./v3/.documentation/package/VERSION.md)                        | Version metadata and helpers                                                             |
| WIRING      | [code](./v3/wiring/) · [docs](./v3/.documentation/package/WIRING.md)                          | Container autowiring code generation: constructor scanning, bind sets, generate command  |

## Extensibility

Melody is extended primarily through:

- **Modules**: register services and configuration defaults.
- **Services**: your container registrations (including overriding framework defaults where supported).
- **Events**: subscribe to lifecycle and domain events.
- **HTTP middleware**: compose request behavior around handlers.
- **CLI commands**: register commands within the CLI integration.

Some APIs are intentionally closed to keep behavior deterministic and to avoid dependency on internal wiring. When an extension point exists, it is documented explicitly in the relevant package documentation.

## Integrations

Optional modules connect Melody to third-party systems (databases, Redis, message brokers, object storage, observability). Each is a separate Go module, so you only pull in what you use.

See the [integrations index](./integrations/) for the full list, supported version lines, and per-integration documentation.

## Build tags

Two independent build tags control what is embedded into the binary, so you can embed one and keep the other on the filesystem:

| Build tag                | Without the tag (default, local development)    | With the tag                                         |
|--------------------------|-------------------------------------------------|------------------------------------------------------|
| `melody_env_embedded`    | Reads `.env` files (`.env`, `.env.local`, ...) from disk | Embeds the `.env` content into the binary (Go `embed`) |
| `melody_static_embedded` | Serves static assets from disk (e.g. `public/`) | Embeds static assets into the binary (Go `embed`)    |

```bash
# Nothing embedded
go build -o app .

# Embed the environment configuration only
go build -tags melody_env_embedded -o app .

# Embed static assets only
go build -tags melody_static_embedded -o app .

# Embed both
go build -tags "melody_env_embedded melody_static_embedded" -o app .
```

For the same build-tag matrix applied end-to-end in a real application, see the [v3 example README](./v3/.example/README.md).

## Example application

The full userland showcase lives under [`./v3/.example/`](./v3/.example/). Start with its [README](./v3/.example/README.md).

## Versions & support

| Version line | Module path                                              | Released              | Latest      | Status                                   | Supported until                     |
|--------------|----------------------------------------------------------|-----------------------|-------------|------------------------------------------|-------------------------------------|
| v4           | `github.com/precision-soft/melody/v4`                    | **Q1 2027** (planned) | —           | Planned; no code yet                     | —                                   |
| **v3**       | `github.com/precision-soft/melody/v3` ([`./v3/`](./v3/)) | 2026-03-08            | **v3.14.6** | **Stable — use for new projects**        | 18 months after v4 ships            |
| v2           | `github.com/precision-soft/melody/v2` ([`./v2/`](./v2/)) | 2026-02-17            | v2.13.4     | Feature-frozen since 2026-08-18          | **2027-09-08**                      |
| v1           | `github.com/precision-soft/melody` (repository root)     | 2026-01-17            | v1.19.4     | Feature-frozen since 2026-08-18          | **2027-08-17**                      |

**Support policy:** the current line gets new features, security fixes, and deprecations. When a new
major ships, the previous line gets security and patch-level fixes for **18 more months**.
v3.14.0 was v3's last feature release; new features ship in v4. Every deprecated v3 symbol names its
replacement, so adopting the replacements now makes the move to v4 painless.

Upgrading from a frozen line? Start at "Migrating to v3" in the
[v1 upgrade guide](./.documentation/UPGRADE.md) or the [v2 upgrade guide](./v2/.documentation/UPGRADE.md).

The full policy (how the dates are derived, and why the three lines are self-contained copies that will not
be consolidated) is in [`.documentation/VERSIONING.md`](./.documentation/VERSIONING.md).

## Documentation

- Package documentation (API reference): [`v3/.documentation/package/`](./v3/.documentation/package/)
- Example application: [`v3/.example/README.md`](./v3/.example/README.md)
- Versioning and support policy: [`.documentation/VERSIONING.md`](./.documentation/VERSIONING.md)
- Roadmap: [`.documentation/ROADMAP.md`](./.documentation/ROADMAP.md)
- Security policy: [`SECURITY.md`](./SECURITY.md)

## Contributing

Development workflow, code style, and documentation rules are in [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Getting help

- Found a bug or have a question? [Open an issue](https://github.com/precision-soft/melody/issues).
- Found a security problem? Do not open a public issue; follow [`SECURITY.md`](./SECURITY.md).

## License

Melody is released under the [MIT License](LICENSE).

<details>
<summary><strong>Development history</strong></summary>

Melody was developed through internal, beta, and release-candidate phases in a GitLab repository, where the architectural evolution, design decisions, and refactors leading up to v1.0.0 took place.

This GitHub repository is the **first stable public release** of Melody, starting at **v1.0.0**, published with a clean history focused on long-term stability and adoption.

The full development history is at https://gitlab.com/precision-soft-open-source/go/melody

</details>
