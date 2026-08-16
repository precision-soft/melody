# internal

Package: [`internal/`](../../internal)

The `internal` package contains framework-internal helper utilities that are **not** intended for userland consumption. Its APIs may change without notice.

## Scope

- Small shared helpers used across Melody packages.
- Test-only helpers under [`internal/testhelper/`](../../internal/testhelper).

## Notable semantics

### Deep copy ([`internal/copy.go`](../../internal/copy.go))

`CopyAnyMap`/`CopyAnySlice` descend into maps and slices **only** — a pointer, or a struct or array holding one, is returned as-is, which is the documented boundary of what session data may safely carry. The traversal memoizes visited nodes, so a node reached through two edges is copied **once** and stays shared inside the copy, a cycle closes onto its own copy rather than onto the live original, and the cost is linear in distinct nodes (the depth-only form was exponential on shared substructure). A depth bound remains as a safety net for genuinely deep data.

### Conversions ([`internal/parse.go`](../../internal/parse.go))

- `Duration` refuses a bare `int`/`int64`: a number without a unit used to be read as **nanoseconds** — a timeout that fired instantly. Pass a `time.Duration` or a string with a unit (`"30s"`).
- `Float64` refuses NaN and the infinities on every branch, including the `"NaN"`/`"Inf"` spellings `strconv.ParseFloat` accepts: a NaN that slips through disarms every ordered comparison downstream.
- `Int` refuses a non-integral, non-finite or out-of-range `float64` with three distinct causes, and the refused scalar value travels in the error context.
- `MapStringString` reports a typed-nil map as **absent**, like the strict accessors beside it.

## Subpackages

### testhelper

Package: [`internal/testhelper/`](../../internal/testhelper)

Test utilities used by Melody unit tests.

Notable helpers:

- `AssertPanicsWithError` for panic expectations in tests, pinning the panic's identity by message — a value-blind assertion cannot tell the guard under test firing from the code crashing for an unrelated reason. ([`internal/testhelper/assert_panics.go`](../../internal/testhelper/assert_panics.go))
- Embedded filesystem helpers that switch behavior under build tags, used by tests that need a deterministic filesystem view. ([`internal/testhelper/embedded_fs_default.go`](../../internal/testhelper/embedded_fs_default.go), [`internal/testhelper/embedded_fs_env_embedded.go`](../../internal/testhelper/embedded_fs_env_embedded.go), [`internal/testhelper/embedded_fs_static_embedded.go`](../../internal/testhelper/embedded_fs_static_embedded.go), [`internal/testhelper/embedded_fs_env_and_static_embedded.go`](../../internal/testhelper/embedded_fs_env_and_static_embedded.go))
