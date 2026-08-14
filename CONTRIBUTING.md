# CONTRIBUTING

This document describes local development, testing, and contribution rules for Melody.

## Versioning and where to make changes

Melody ships as three parallel Go module lines (see the [`README.md`](./README.md#versions--project-status)):

- **v3** (`./v3/`) — stable, actively maintained. **All new features go here.**
- **v2** (`./v2/`) and **v1** (repository root) — maintenance mode.

Rules for contributions:

- **New features:** v3 only. Do not add features to v1 or v2.
- **Bug fixes:** apply to v3. **Back-port to v1 and v2 only when the fix is security-related or a critical correctness issue.** Other fixes stay on v3.
- **Breaking changes:** instead of changing a v3 API in place, mark the old form with a `/* Deprecated: ... */`
  doc comment and keep it working; breaking changes accumulate toward a future v4.
- The three versions are **intentionally duplicated** so each binds to one framework version. Do not try to consolidate or de-duplicate them — such pull requests will not be accepted.

When a change touches multiple version lines, keep each line's edit self-contained and update each line's
`CHANGELOG.md`.

## Development setup

Prerequisites:

- Go (this repository is a Go workspace of several modules; see [`go.work`](./go.work) — the root [`go.mod`](./go.mod) is the v1 framework module)
- Docker (required for the verification gate: [`.dev/validate/all.sh`](./.dev/validate/all.sh) runs every check inside the development container, and the git hooks `./dc` installs invoke it on commit and push)

The quickest way into the development shell is the [`./dc`](./dc) wrapper, which also installs the repository git hooks:

```bash
./dc up:minimal   # core development shell
./dc up:all       # plus integration services (RabbitMQ, Redis, MySQL, LocalStack, PostgreSQL, Mailpit, Prometheus, the OTLP collector)
```

Inside the shell, the convenience functions described under
[Development shell aliases](#development-shell-aliases) run the verification matrix for the module you are in; the whole-repository gate is `melody_validate_all` (alias `mva`, staged-files variant `mvs`), which delegates to [`.dev/validate/all.sh`](./.dev/validate/all.sh).

### Overriding host ports

The committed [`.dev/docker/.env`](./.dev/docker/.env) carries the default host ports the compose file reads (the HTTP load balancer on `80`, the example app on `8180`, and the `up:all` services on
`5673`/`15673` (RabbitMQ), `6380` (Redis), `3307` (MySQL), `4566` (LocalStack)); the compose file supplies its own defaults for the rest — `5433` (PostgreSQL), `1026`/`8026` (Mailpit), `9091` (Prometheus), `4317`/`4318` (the OTLP collector) — and the per-major `dev-v1`/`dev-v2` shells publish no host port. Each port is read in [`docker-compose.yml`](./.dev/docker/docker-compose.yml) as a `${VAR:-default}` lookup, so you can move any of them without editing the committed file: create an uncommitted `.dev/docker/.env.local`
(gitignored, loaded after `.env` so it wins) and set only the ports you need to change. This is the supported way to run the melody dev stack alongside other local stacks that already use those ports.

```bash
# .dev/docker/.env.local  (gitignored, per-developer)
LOAD_BALANCER_HTTP_HOST_PORT=8095
DEV_HTTP_HOST_PORT=8185
RABBITMQ_HOST_PORT=5674
RABBITMQ_MANAGEMENT_HOST_PORT=15674
REDIS_HOST_PORT=6381
MYSQL_HOST_PORT=3308
LOCALSTACK_HOST_PORT=4567
```

## Build tags and verification matrix

Melody supports two independent embedding modes controlled by build tags:

- Environment embedding: `melody_env_embedded` (see [`./application/environment_embedded.go`](./application/environment_embedded.go))
- Static embedding: `melody_static_embedded` (see [`./application/static_embedded.go`](./application/static_embedded.go))

All changes must be tested and vetted under **all supported build-tag combinations**, for every workspace module the change touches — the enforced gate, [`.dev/validate/all.sh`](./.dev/validate/all.sh), runs the matrix for the framework majors (repository root, [`v2/`](./v2/), [`v3/`](./v3/)), their example applications, and every integration module. The commands below show the matrix for the v1 pair:

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
- `gall`: run the three embedded modes (env, static, both) — the default no-tag combination is `goa`, run separately
- `melody_validate_all` / `mva` (staged-files variant `melody_validate_staged` / `mvs`): the whole-repository gate, delegating to `.dev/validate/all.sh`

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
- Use `/* ... */` for comments; do not use `//` (except for Go build/tool directives such as `//go:build` and `//go:embed`, the v3 wiring markers `//melody:service`/`//melody:scoped`/`//melody:bind` — which the wiring scanner reads from line comments only — and a linter directive such as `//nolint`).
- A comment is the exception, not the norm. Where the code is clear, write none; where a comment stays, it states the constraint the code *currently* carries and cannot show for itself — not the history of a repair, and not what the code used to do. The long "why" belongs in `CHANGELOG.md` and in `.documentation/`.
- In a test, the test name is the explanation. A comment there earns its place only by saying something about the *test* that the test cannot show: that the guard it drives is shadowed by a sibling, that the branch is unreachable through the public API, that a probe is shaped a particular way on purpose, or an external fact the code is aligned to.
- Annotation markers (`@info`, `@important`, `@todo`) are not used.
- Deprecations use the Go-standard marker as a `/* ... */` block — a doc comment whose first paragraph begins with `Deprecated:` (for example `/* Deprecated: use NewThing instead. */`). This is machine-recognized by `go doc`, `gopls`, and `staticcheck`, and renders correctly on `pkg.go.dev`.

#### Function/method formatting

- If a function/method call is split across multiple lines, **each parameter must be on its own line**, and the closing parenthesis must be on a separate line.

## Reporting bugs

When submitting a bug report, include:

- The exact Melody version (tag/commit)
- Go version and OS
- Clear reproduction steps (minimal example if possible)
- The observed behavior and the expected behavior
- Relevant logs and stack traces (redact secrets)

If the issue is security-sensitive, do not file it publicly; follow [`SECURITY.md`](./SECURITY.md).

## Submitting pull requests

- Use a topic branch based on `main`.
- Keep the PR focused: one logical change-set per PR.
- Add or update tests for behavioral changes.
- If the change affects userland behavior, update the relevant documentation under [`./.documentation/`](./.documentation/) and, when applicable, the example app docs under [`./.example/`](./.example/).

## Security and support

- For security issues, follow [`SECURITY.md`](./SECURITY.md): report privately through GitHub's private vulnerability reporting with a minimal reproduction and impact assessment. Do not open a public issue.
- For non-security questions, use the standard issue tracker and include context (version, steps, logs).
