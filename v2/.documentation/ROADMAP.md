# ROADMAP

This document lists high-level, forward-looking plans for the Melody repository.

The items this document used to carry have shipped: static file serving has first-class filesystem and embedded modes behind the `melody_static_embedded` build tag, the firewall system is the [`security`](../security/) package (see [`./package/SECURITY.md`](./package/SECURITY.md)), and the router carries named routes, url generation, route grouping and constraints (see [`../http/router.go`](../http/router.go), [`../http/router_group.go`](../http/router_group.go), [`../http/constraint.go`](../http/constraint.go) and [`./package/HTTP.md`](./package/HTTP.md)).

## Direction

- This major line is being driven to complete stability and then frozen; new capabilities land in the newest major line. Remaining work here is hardening: aligning behaviour with what the documentation promises, closing defects, and keeping the example application a faithful showcase.

## Longer-term

- Close remaining gaps
    - Incrementally add framework capabilities that are aligned with Melody’s core principles (determinism, explicit wiring, clear boundaries), without expanding internal-only APIs into userland unintentionally. See [`../application/`](../application/) and [`../kernel/`](../kernel/).
