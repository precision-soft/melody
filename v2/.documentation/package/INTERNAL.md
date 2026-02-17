# internal

Package: [`internal/`](../../internal)

The `internal` package contains framework-internal helper utilities that are **not** intended for userland consumption. Its APIs may change without notice.

## Scope

- Small shared helpers used across Melody packages.
- Test-only helpers under [`internal/testhelper/`](../../internal/testhelper).

## Subpackages

### testhelper

Package: [`internal/testhelper/`](../../internal/testhelper)

Test utilities used by Melody unit tests.

Notable helpers:

- `AssertPanics` for panic expectations in tests. ([`internal/testhelper/assert_panics.go`](../../internal/testhelper/assert_panics.go))
- Embedded filesystem helpers that switch behavior under build tags, used by tests that need a deterministic filesystem view.
  ([`internal/testhelper/embedded_fs_default.go`](../../internal/testhelper/embedded_fs_default.go), [`internal/testhelper/embedded_fs_env_embedded.go`](../../internal/testhelper/embedded_fs_env_embedded.go), [`internal/testhelper/embedded_fs_static_embedded.go`](../../internal/testhelper/embedded_fs_static_embedded.go), [`internal/testhelper/embedded_fs_env_and_static_embedded.go`](../../internal/testhelper/embedded_fs_env_and_static_embedded.go))
