# ROADMAP

This document lists high-level, forward-looking plans for the Melody repository.

Every item this document used to carry has shipped — static file serving with its filesystem and embedded
modes, the firewall system, and the router's named routes, url generation, grouping and constraints. What
they do now is described where a current capability belongs, in [`./package/`](./package/), not here.

## Longer-term

- Close remaining gaps
    - Incrementally add framework capabilities that are aligned with Melody’s core principles (determinism, explicit wiring, clear boundaries), without expanding internal-only APIs into userland unintentionally. See [`../application/`](../application/) and [`../kernel/`](../kernel/).
