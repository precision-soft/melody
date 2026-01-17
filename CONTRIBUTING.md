# CONTRIBUTING

This document describes local development, testing, and contribution rules for Melody.

## Development setup

Prerequisites:

- Go (this repository is a Go module; see [`go.mod`](./go.mod))
- Make (optional; used only if you rely on repository scripts)
- Docker (optional; used only for the repository development shell under [`.dev/`](./.dev/))

## Build tags and verification matrix

Melody supports two independent embedding modes controlled by build tags:

- Environment embedding: `melody_env_embedded` (see [`./application/environment_embedded.go`](./application/environment_embedded.go))
- Static embedding: `melody_static_embedded` (see [`./application/static_embedded.go`](./application/static_embedded.go))

All changes must be tested and vetted under **all supported build-tag combinations**, for both:

- the framework (repository root)
- the example application ([`.example/`](./.example/))

### Required commands

Default (no build tags):

```bash
go test ./...
go vet ./...

(
  cd .example
  go test ./...
  go vet ./...
)
```

`melody_env_embedded`:

```bash
go test -tags melody_env_embedded ./...
go vet -tags melody_env_embedded ./...

(
  cd .example
  go test -tags melody_env_embedded ./...
  go vet -tags melody_env_embedded ./...
)
```

`melody_static_embedded`:

```bash
go test -tags melody_static_embedded ./...
go vet -tags melody_static_embedded ./...

(
  cd .example
  go test -tags melody_static_embedded ./...
  go vet -tags melody_static_embedded ./...
)
```

`melody_env_embedded` + `melody_static_embedded`:

```bash
go test -tags "melody_env_embedded melody_static_embedded" ./...
go vet -tags "melody_env_embedded melody_static_embedded" ./...

(
  cd .example
  go test -tags "melody_env_embedded melody_static_embedded" ./...
  go vet -tags "melody_env_embedded melody_static_embedded" ./...
)
```

## Development shell aliases

The repository includes a Docker-focused development shell profile at [`./.dev/docker/.profile`](./.dev/docker/.profile).

It defines convenience functions for the verification matrix:

- `gv` / `gt`: run `go vet` / `go test`
- `goa`: run `gv` then `gt`
- `gaee`, `gase`, `gaes`: run `goa` with `melody_env_embedded`, `melody_static_embedded`, or both
- `gall`: run all embedded modes (env, static, both)

It also defines build helpers that produce executable binaries:

- `gbam`: build default + all embedded modes (see `go_build_all_embedded_modes()` in the same file)

## Development workflow

Before opening a pull request:

1. Run the full verification matrix (see [Build tags and verification matrix](#build-tags-and-verification-matrix)).
2. Keep changes scoped. Avoid drive-by refactors unless they are required for the change.
3. Update documentation when behavior, invariants, or public APIs change.
    - Package documentation lives under [`./.documentation/package/`](./.documentation/package/).
    - General documentation rules live in [`./.documentation/DOCUMENTATION.md`](./.documentation/DOCUMENTATION.md).

## Code style

### Melody code style (normative)

The repository enforces a strict, opinionated style. Contributions are expected to follow these rules.

#### Go style and structure

- Package/file/type names use **singular** form (no plural directories/types).
- Prefer **one major type per file**; avoid “god files”. If multiple types must coexist, group them by responsibility.
- In struct-heavy files, ordering must be **exported → unexported**, consistently.
- In Go methods with pointer receivers, the receiver variable name must be `instance`.
- Avoid defensive nil checks and implicit instantiations in framework-owned codepaths where a failure would indicate incorrect API usage.

#### Naming conventions

- Use **camelCase** consistently for identifiers.
- Avoid abbreviations (prefer descriptive names). The exception is the well-known Go convention `err` where it is the single obvious error in scope.
- Acronyms must follow camelCase rules (for example: `urlString`, `httpClient`, `jsonDecoder`, `userId`).
- For error variables: prefer meaningful names (for example: `dispatchErr`, `validationErr`) when multiple errors are in scope; use `err` only when it is the single obvious error.

#### Comparisons and boolean logic

- Apply **Yoda style** universally for comparisons (constant on the left side).
- Do not use the `!` negation operator in logic; express conditions explicitly instead.

#### Errors and messages

- Error messages must be **lowercase-only**.
- When fail-fast behavior is required, do not use raw `panic` directly; use the framework’s exception mechanism (see [`./exception/`](./exception/) and [`.documentation/package/EXCEPTION.md`](./.documentation/package/EXCEPTION.md); for example `exception.NewError` + `exception.Panic`).

#### Comments

- All comments must be in **English**.
- Use `/** ... */` for comments; do not use `//` (except for Go build tags).
- Permanent comments should be used only exceptionally. Annotation comments (`@todo`, `@important`, etc.) must also use `/** ... */`.

#### Function/method formatting

- If a function/method call is split across multiple lines, **each parameter must be on its own line**, and the closing parenthesis must be on a separate line.

## Reporting bugs

When submitting a bug report, include:

- The exact Melody version (tag/commit)
- Go version and OS
- Clear reproduction steps (minimal example if possible)
- The observed behavior and the expected behavior
- Relevant logs and stack traces (redact secrets)

If the issue is security-sensitive, do not file it publicly; follow the security reporting guidance below.

## Submitting pull requests

- Use a topic branch based on `master`.
- Keep the PR focused: one logical change-set per PR.
- Add or update tests for behavioral changes.
- If the change affects userland behavior, update the relevant documentation under [`./.documentation/`](./.documentation/) and, when applicable, the example app docs under [`./.example/`](./.example/).

## Security and support

- For security issues, report privately with a minimal reproduction and impact assessment.
- For non-security questions, use the standard issue tracker and include context (version, steps, logs).
