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

The example lives entirely under the [`./.example/`](./) directory.  
All paths below are **relative to `.example/`**.

```
.example/
├── bootstrap/         # Application wiring and integration glue
├── domain/            # Domain models and services (product, category, currency, user)
├── infra/http/        # HTTP wiring: routes, handlers, middleware, security
├── infra/command/     # CLI commands specific to the example application
├── public/            # Static assets (CSS / JS) for filesystem static mode
├── embedded_*         # Build-tag–controlled embedding for env and static assets
└── main.go            # Application entry point
```

### High-level responsibilities

- [`domain/`](./domain/)  
  Contains pure domain logic:
    - domain models
    - in-memory repositories
    - domain services

  This layer is framework-agnostic and is wired into the application via the Melody container.

- [`infra/http/`](./infra/http/)  
  Contains all HTTP-related infrastructure:
    - route registration
    - HTTP handlers (pages and JSON APIs)
    - middleware
    - security access control rules
    - session and authentication wiring

  This layer adapts HTTP requests to domain services.

- [`infra/command/`](./infra/command/)  
  Contains CLI commands specific to the example application.

  Commands:
    - run inside a Melody runtime
    - have access to the container and services
    - demonstrate Melody CLI conventions and patterns

- [`public/`](./public/)  
  Static assets (CSS / JS) served by Melody when **static embedding is disabled**.

- [`embedded_*`](./)  
  Build-tag–controlled files that decide whether:
    - environment configuration (`.env`) is loaded from filesystem or embedded
    - static assets (`public/`) are served from filesystem or embedded

---

### [`bootstrap/`](./bootstrap/)

The [`bootstrap/`](./bootstrap/) package contains the **example application wiring**.

Its purpose is to keep [`main.go`](./main.go) small and declarative by grouping all setup and integration logic in a single place.

[`main.go`](./main.go) creates the Melody application instance and delegates all configuration to this package.

**Where it is used:**  
[`main.go`](./main.go) calls `bootstrap.Configure(app)`.

`bootstrap.Configure(...)` is implemented in [`./bootstrap/configure.go`](./bootstrap/configure.go).

#### Responsibilities

The [`bootstrap/`](./bootstrap/) package is responsible for:

- registering domain services into the container
- assembling the example’s dependency graph
- registering the example module
- wiring HTTP routes, security, and middleware
- registering example CLI commands

It contains **no business logic**.

#### Files

- [`configure.go`](./bootstrap/configure.go)  
  Entry point for example wiring.

  This is the single function invoked by [`main.go`](./main.go).  
  It orchestrates the entire setup process by:
    - registering services
    - registering the example module
    - registering example HTTP middleware

- [`service.go`](./bootstrap/service.go)  
  Container and service registration for the example domain.

  Registers:
    - cache serializer (example implementation)
    - in-memory repositories (category, currency, product, user)
    - domain services:
        - `CategoryService`
        - `CurrencyService`
        - `UserService`
        - `ProductService`

  Services are wired with:
    - repositories
    - Melody cache
    - Melody event dispatcher

  This file defines the example’s full dependency injection graph.

- [`module.go`](./bootstrap/module.go)  
  Defines the example module (`NewExampleModule`) and integrates the example with Melody’s kernel extension points.

  Responsibilities include:
    - **Security**:
        - access control rules
        - role hierarchy
        - access decision manager
        - entry point and access denied handler
        - firewall configuration
    - **HTTP**:
        - registration of named routes
        - registration of page handlers and JSON API handlers
    - **CLI**:
        - registration of example CLI commands
    - **Events**:
        - registration of event subscribers for product, category, currency, user, and security events

  This file is the primary integration point between the example application and the Melody framework.

- [`middleware.go`](./bootstrap/middleware.go)  
  Defines example-specific HTTP middleware.

  Currently contains:
    - `NewTimingMiddleware()`: measures request duration and sets the
      `X-Example-Duration-Ms` response header when a response exists.

  Middleware is registered via `bootstrap.Configure()`.

---

### [`main.go`](./main.go) (why it stays small)

`main.go` intentionally contains minimal logic.

It only:

- constructs the Melody application using:
    - `embeddedEnvFiles` (from `embedded_env_*`)
    - `embeddedPublicFiles` (from `embedded_static_*`)
- calls `bootstrap.Configure(app)`
- runs the application (`app.Run(ctx)`)

All wiring and integration logic lives outside `main.go`.

---

## Running locally (filesystem mode)

From the repository root:

```bash
cd .example
go run .
```

Once started, open the application in your browser:

- http://localhost:8080

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
