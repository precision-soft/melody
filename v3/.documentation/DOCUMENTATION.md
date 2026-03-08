# DOCUMENTATION

This document defines the canonical documentation rules for the Melody repository.

It is the only place where documentation-writing guidelines, checklists, and author instructions may live.
User-facing documentation files must not contain meta-text, writing prompts, or author instructions.

All documentation files are written in English.

## Documentation archetypes

### [`.example/README.md`](../.example/README.md)

Purpose: document the example application as a userland showcase.

Include:

- Context & purpose (what it is, what it is not)
- Conceptual overview (high level, descriptive)
- Feature enumeration (descriptive, not API)
- Operational conveniences (demo credentials, default URLs, ports)
- Structure overview (directory tree + responsibilities)

Exclude:

- Framework API reference
- Framework internals
- Author guidelines / checklists

### [`README.md`](../README.md) (repository root)

Purpose: framework entry document.

Include:

- Positioning and scope (what Melody is / is not)
- Core principles
- Conceptual architecture (runtime/container/kernel/event/http)
- Build & packaging concepts (build tags, embedded vs filesystem modes)
- Navigation pointers to package documentation and the example

Exclude:

- Per-package API reference
- Deep internal details

### [`CONTRIBUTING.md`](../../CONTRIBUTING.md)

Purpose: contribution and process guidance.

Include:

- Contribution scope
- Code style & rules
- Architectural constraints
- Testing expectations
- Review/process discipline

### [`.documentation/DOCUMENTATION.md`](./DOCUMENTATION.md)

Purpose: documentation canon (this file).

### [`.documentation/ROADMAP.md`](./ROADMAP.md)

Purpose: repository roadmap (future plans only).

Include:

- High-level future capabilities and sequencing
- Links to relevant packages or directories where applicable

Exclude:

- Current API reference
- Author guidelines / checklists

### [`.documentation/package/`](./package/)

Purpose: API-driven documentation for a package and its subpackages.

These documents must be short, factual, and scoped strictly to the package and its subpackages.

## Global rules

### Traceability (required)

All references in Markdown must include **relative links** to the corresponding source code locations
(files or directories) whenever the text references:

- a package or subpackage
- a source file
- a public API symbol
- a behavior that is implemented in a specific location

This is required to prevent documentation–code drift and to make navigation deterministic.

### File naming

- Markdown file base names are uppercase (for example: `README.md`, `HTTP.md`).
- Package docs live only under [`.documentation/package/`](./package/).

### Code examples

All code snippets and examples in Markdown must strictly follow the same code style rules as the Melody framework itself.
Examples are treated as production code, not pseudo-code.

This includes (non-exhaustive):

- Naming conventions (camelCase, acronym casing, no unnecessary abbreviations, singular names)
- Ordering rules used in the repository
- Yoda style comparisons
- Do not use the `!` negation operator in logic
- Comments must use `/** ... */` only (avoid `//`)
- Error messages must be lowercase-only
- Multi-line calls must place one parameter per line, with the closing parenthesis on its own line

### Omit empty sections

Sections with no content must be omitted entirely (do not write “None”).

## Local development requirements

Whenever any documentation mentions `go test` or `go vet`, it must:

- List all supported build-tag combinations: default (no tags), `melody_env_embedded`, `melody_static_embedded`, and `melody_env_embedded` + `melody_static_embedded`.
- State explicitly that both the framework (repository root) and the example application ([`./.example/`](../.example/)) must be tested and vetted under the full matrix.
- Link to the local development shell aliases defined in [`./.dev/docker/.profile`](../.dev/docker/.profile) when referencing convenience commands.

The normative build tags are implemented in [`../application/environment_embedded.go`](../application/environment_embedded.go) and [`../application/static_embedded.go`](../application/static_embedded.go).

## Package documentation requirements

### Scope and boundaries

Each package document must:

- Cover only the package and its subpackages
- Explicitly distinguish userland-intended exports from framework-internal integration points when relevant
- Avoid unrelated notes

### Subpackages

If the package contains relevant subpackages, include a “Subpackages” section listing all subpackages (no selective lists).
For each subpackage, include a short description of its purpose and boundary (userland vs framework).

### Configuration

If the package has any user-configurable knobs (via `.env` artifacts, module methods, config structs, or container wiring),
include a “Configuration” section describing:

- Available options
- Defaults
- How the option is set (where it is read/applied)

### Usage examples

Usage examples must be representative and end-to-end, showing how Melody is intended to be used:

- Wiring (modules/services)
- Configuration (when applicable)
- Retrieving dependencies from container/scope/runtime (when applicable)
- Real usage in a realistic flow

Avoid standalone “package-only” snippets that ignore container/runtime integration.

Internal-only packages must not include usage examples.

### API reference: semantic grouping

Exported API must be grouped semantically (for example: Routing, Middleware, Url generation, Responses, Constraints, Stores).
Avoid flat, undifferentiated lists.

Functions, types, and errors should be grouped consistently within the package’s domain.

### Constructors vs retrieval helpers (strict separation)

- Constructors (`NewXxx`, factories) are documented alongside their associated types.
- Retrieval helpers such as `XxxFromContainer`, `MustXxxFromContainer`, `XxxFromScope`, `XxxFromRuntime`
  are not constructors and must be documented in a dedicated section (for example: “Container / Scope / Runtime access”).

If a feature can be both constructed and retrieved from container/scope/runtime, document both approaches.

### Userland API at the end (uniform)

For packages that expose userland API, include a “Userland API” section placed at the end of the document.
This section lists only the exports intended for userland use, grouped semantically.

Internal-only packages must not include a “Userland API” section.

### Footguns & caveats (optional)

Include this section only when real, to document sharp edges such as:

- Fail-fast behavior (`Must*` resolvers, exception-based panic)
- Lifecycle requirements (what must be `Close()`d and when)
- Determinism and ordering guarantees
- Concurrency guarantees (boot-time only vs concurrent-safe)
- Intentionally strict vs intentionally permissive behavior
