# BOUND

The [`bound`](../../bound) package provides the three-state limit every melody limit and timeout is written with: absent, a value, or unlimited.

## Scope

A configuration struct carries its limits and timeouts as `bound.Duration` or `bound.Count`. Go cannot tell a field left out from a field set to zero, and zero is a meaningful value for some limits and a dangerous one for others, so the state is explicit:

- a field left out is **absent** and takes the framework's bounded default;
- [`Of`](../../bound/limit.go) sets a **value**, zero included where the field admits it;
- [`UnlimitedDuration`](../../bound/limit.go) and [`UnlimitedCount`](../../bound/limit.go) lift the limit, on the fields that admit **unlimited**;
- a value below the field's minimum, and unlimited on a field that does not admit it, are refused when the component is built, naming the field.

`bound` is used only on limits and timeouts, where zero is ambiguous. A flag is a `bool` whose name makes its zero value the safe default (`RedirectsDisabled`, `KeepAliveDisabled`).

## Responsibilities

- The limit types:
    - [`Limit`](../../bound/limit.go), with the aliases [`Duration`](../../bound/limit.go) and [`Count`](../../bound/limit.go)
    - [`Of`](../../bound/limit.go), [`UnlimitedDuration`](../../bound/limit.go), [`UnlimitedCount`](../../bound/limit.go)
- Reading a limit from configuration:
    - `UnmarshalText` / `MarshalText` and `UnmarshalJSON` / `MarshalJSON` in [`encoding.go`](../../bound/encoding.go)
- Resolving a field:
    - [`Rule`](../../bound/resolve.go), [`Resolve`](../../bound/resolve.go), [`Resolved`](../../bound/resolve.go)
    - [`TimeoutMinimum`](../../bound/resolve.go) (1ms) and [`TtlMinimum`](../../bound/resolve.go) (1s), the two minimum classes

## Usage

```go
package main

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/httpclient"
)

func newExportClient() (*httpclient.HttpClient, error) {
    return httpclient.NewHttpClient(
        httpclient.HttpClientConfig{
            Timeout: bound.Of(5 * time.Second),
            Transport: httpclient.TransportConfig{
                IdleConnTimeout: bound.UnlimitedDuration(),
            },
        },
    )
}
```

## Footguns & caveats

- **The zero value is absent, not zero.** `bound.Duration{}` takes the default; a zero is written `bound.Of(0)` on a count and `bound.Of(time.Duration(0))` on a duration, and is refused on a field whose minimum is above it.
- **`Of` infers the unit from its argument.** `bound.Of(5)` is a `Count`; a duration field wants `bound.Of(5 * time.Second)`, and the untyped constant does not compile against it.
- **The text form has one spelling of unlimited.** `UnmarshalText` trims the surrounding whitespace, then reads `unlimited`, a value or the empty text as absent: a duration in `time.ParseDuration`'s form (`30s`, and the bare `0` it admits), a count as `strconv.Atoi` reads it (`64`, `+64`); any other text, `Unlimited` or a duration such as `30` without its unit included, is refused. A json `null` is absent, a json number reads only into a `Count`, and a missing key leaves the field absent. An absent limit is written back as `null`; tag the field `omitzero` to leave it out instead, since `omitempty` never omits a struct.
- **The minimum is the field's, not the type's.** Timeouts, budgets and delays default to a 1ms minimum and ttls, retentions and expiries to 1s; a field that admits zero as a value says so in its documentation.

## Userland API

- [`type Limit[T Unit]`](../../bound/limit.go), [`type Duration`](../../bound/limit.go), [`type Count`](../../bound/limit.go), [`type Unit`](../../bound/limit.go)
    - [`Of[T Unit](value T) Limit[T]`](../../bound/limit.go)
    - [`UnlimitedDuration() Duration`](../../bound/limit.go)
    - [`UnlimitedCount() Count`](../../bound/limit.go)
    - `IsAbsent() bool`, `IsUnlimited() bool`, `Value() (T, bool)`, `String() string`
    - `MarshalText`, `UnmarshalText`, `MarshalJSON`, `UnmarshalJSON`
- [`type Rule[T Unit]`](../../bound/resolve.go) — `Name`, `Default`, `Minimum`, `UnlimitedAdmitted`
- [`type Resolved[T Unit]`](../../bound/resolve.go) — `Value`, `Unlimited`
- [`Resolve[T Unit](limit Limit[T], rule Rule[T]) (Resolved[T], error)`](../../bound/resolve.go)
- [`TimeoutMinimum`](../../bound/resolve.go), [`TtlMinimum`](../../bound/resolve.go)
