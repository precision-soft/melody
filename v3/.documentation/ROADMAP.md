# ROADMAP

This document lists high-level, forward-looking plans for the Melody repository.

## Near-term

- Static file serving enhancements
    - Add explicit support for filesystem and embedded modes as a first-class feature set, aligned with the existing build tags (`melody_static_embedded`). See [`../http/`](../http/) and [`./package/HTTP.md`](./package/HTTP.md).

- Firewall system
    - Introduce a configurable firewall layer that can be composed with HTTP middleware and security integration points. See [`../security/`](../security/) and [`./package/SECURITY.md`](./package/SECURITY.md).

## Mid-term

- Router extensions
    - Named routes, url generation, route grouping, and constraints. See [`../http/router.go`](../http/router.go) and [`./package/HTTP.md`](./package/HTTP.md).

## Longer-term

- Close remaining gaps
    - Incrementally add framework capabilities that are aligned with Melody’s core principles (determinism, explicit wiring, clear boundaries), without expanding internal-only APIs into userland unintentionally. See [`../application/`](../application/) and [`../kernel/`](../kernel/).
