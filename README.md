# Melody

Melody is a Go framework focused on building **HTTP applications and CLI commands** on top of the same runtime, container, configuration, logging, and validation infrastructure.

The repository also contains a complete userland showcase under [`./.example/`](./.example/).

## Why Melody

Melody is designed for teams that want:

- A single **service container** and **runtime lifecycle** shared by HTTP and CLI entrypoints.
- Deterministic wiring: behavior is assembled through modules, services, and explicit registration rather than global state.
- Clear boundaries between userland APIs (what you build on) and framework internals (what you do not depend on).

## Architecture

At a high level, a Melody application is assembled as follows:

- **Application** ([code](./application/)) wires modules and services into a **container** ([code](./container/)).
- A **runtime** ([code](./runtime/)) owns the lifecycle (boot/compile/run/shutdown) and creates request/command scopes.
- **HTTP** ([code](./http/)) uses the runtime + container scopes to run middleware and dispatch handlers.
- **CLI** ([code](./cli/)) runs commands inside the same runtime/container infrastructure.
- Cross-cutting packages are wired as services: [logging](./logging/), [event](./event/), [validation](./validation/), [cache](./cache/), [session](./session/), [security](./security/).

## Extensibility

Melody is extended primarily through:

- **Modules**: register services and configuration defaults.
- **Services**: your container registrations (including overriding framework defaults where supported).
- **Events**: subscribe to lifecycle and domain events.
- **HTTP middleware**: compose request behavior around handlers.
- **CLI commands**: register commands within the CLI integration.

Some APIs are intentionally closed to keep behavior deterministic and to avoid dependency on internal wiring. When an extension point exists, it is documented explicitly in the relevant package documentation.

## Build tags

Melody supports two independent embedding modes controlled by build tags:

1. Environment configuration (`.env`-style files)
2. Static assets (filesystem vs embedded)

These are intentionally independent so you can embed one family while keeping the other on the filesystem.

---

### 1) Environment configuration (`.env`)

**Build tag:**

- `melody_env_embedded`

#### Behavior

- **Without** `melody_env_embedded`  
  Environment configuration is loaded from filesystem `.env` files (for example `.env`, `.env.local`). This is the default for local development.

- **With** `melody_env_embedded`  
  Environment configuration is embedded into the binary at build time (via Go `embed`). The runtime reads the embedded `.env` content instead of the filesystem.

#### Build examples

```bash
# filesystem env (default)
go build -o app ./...

# embedded env
go build -tags melody_env_embedded -o app ./...
```

---

### 2) Static assets

**Build tag:**

- `melody_static_embedded`

#### Behavior

- **Without** `melody_static_embedded`  
  Static assets are served from the filesystem (for example from an application-provided `public/` directory). This is the default for local development.

- **With** `melody_static_embedded`  
  Static assets are embedded into the binary at build time (via Go `embed`). The HTTP layer serves the embedded assets.

#### Build examples

```bash
# filesystem static (default)
go build -o app ./...

# embedded static assets
go build -tags melody_static_embedded -o app ./...
```

---

### Combining build tags

You can combine the tags to embed both families:

```bash
go build -tags "melody_env_embedded melody_static_embedded" -o app ./...
```

For a complete example that shows the same build-tag matrix applied end-to-end in a userland application, see [`.example/README.md`](./.example/README.md).

## Documentation

Melody documentation follows a strict, canonical structure. The documentation canon is defined in [`.documentation/DOCUMENTATION.md`](./.documentation/DOCUMENTATION.md) and is normative for all Markdown files in this repository.

Key entry points:

- Framework entry document: [`README.md`](./README.md)
- Example application documentation: [`.example/README.md`](./.example/README.md)
- Contribution and code style rules: [`CONTRIBUTING.md`](./CONTRIBUTING.md)
- Package documentation (API reference): [`.documentation/package/`](./.documentation/package/)
- Roadmap (future plans): [`.documentation/ROADMAP.md`](./.documentation/ROADMAP.md)

## Packages

Each package below links to its source folder and its package documentation.

* **APPLICATION** — [code](./application/) | [docs](.documentation/package/APPLICATION.md)  
  High-level application bootstrap, module registration, and run modes.

* **BAG** — [code](./bag/) | [docs](.documentation/package/BAG.md)  
  Typed value access patterns and conversion semantics used by configuration.

* **CACHE** — [code](./cache/) | [docs](.documentation/package/CACHE.md)  
  In-process caching contracts and implementations.

* **CLI** — [code](./cli/) | [docs](.documentation/package/CLI.md)  
  CLI contracts, command registration, and execution model.

* **CLOCK** — [code](./clock/) | [docs](.documentation/package/CLOCK.md)  
  Clock abstraction for deterministic time and testing.

* **CONFIG** — [code](./config/) | [docs](.documentation/package/CONFIG.md)  
  Configuration loading and composition (file-based, env artifacts).

* **CONTAINER** — [code](./container/) | [docs](.documentation/package/CONTAINER.md)  
  Dependency injection container, scopes, service factories, and lifecycle.

* **DEBUG** — [code](./debug/) | [docs](.documentation/package/DEBUG.md)  
  Built-in CLI debug commands (container, events, router, middleware, parameters, versions).

* **EVENT** — [code](./event/) | [docs](.documentation/package/EVENT.md)  
  Deterministic event dispatching and subscriber/listener contracts.

* **EXCEPTION** — [code](./exception/) | [docs](.documentation/package/EXCEPTION.md)  
  Error wrappers, context propagation, and fail-fast helpers.

* **HTTP** — [code](./http/) | [docs](.documentation/package/HTTP.md)  
  HTTP server, router integration, middleware execution, request orchestration.

* **HTTPCLIENT** — [code](./httpclient/) | [docs](.documentation/package/HTTPCLIENT.md)  
  Outbound HTTP client contracts and helpers.

* **KERNEL** — [code](./kernel/) | [docs](.documentation/package/KERNEL.md)  
  Kernel integration points that connect application, runtime, and HTTP/CLI wiring.

* **LOGGING** — [code](./logging/) | [docs](.documentation/package/LOGGING.md)  
  Structured logging contracts and framework logging conventions.

* **RUNTIME** — [code](./runtime/) | [docs](.documentation/package/RUNTIME.md)  
  Application runtime lifecycle, boot/compile/run, and wiring orchestration.

* **SECURITY** — [code](./security/) | [docs](.documentation/package/SECURITY.md)  
  Access control rules, authentication integration points, and security wiring.

* **SERIALIZER** — [code](./serializer/) | [docs](.documentation/package/SERIALIZER.md)  
  Serialization contracts and helpers for request/response boundaries.

* **SESSION** — [code](./session/) | [docs](.documentation/package/SESSION.md)  
  Session storage contracts and request/session lifecycle integration.

* **VALIDATION** — [code](./validation/) | [docs](.documentation/package/VALIDATION.md)  
  DTO validation engine, constraints, and errors.

* **VERSION** — [code](./version/) | [docs](.documentation/package/VERSION.md)  
  Version metadata and helpers.

## Example application

The full userland showcase lives under `./.example/`. Start here:

- [`.example/README.md`](.example/README.md)

## Contributing

Development workflow and contribution rules:

- [`CONTRIBUTING.md`](CONTRIBUTING.md)

## Development history

Melody was developed and iterated through multiple internal, beta, and release-candidate phases in a GitLab repository, where the full architectural evolution, design decisions, and refactors leading up to v1.0.0 took place.

This GitHub repository represents the **first stable public release** of Melody, starting with version **v1.0.0**, intentionally published with a clean history focused on long-term stability and user adoption.

If you want to explore the full development history that led to v1.0.0, see:
https://gitlab.com/precision-soft-open-source/go/melody
