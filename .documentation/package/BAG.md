# BAG

The [`bag`](../../bag) package provides typed key–value containers used throughout Melody for configuration, parameters, request-scoped data, and structured access with explicit conversion semantics.

## Scope

- Package: [`bag/`](../../bag)
- Subpackage: [`bag/contract/`](../../bag/contract)

## Subpackages

- [`bag/contract`](../../bag/contract)  
  Public contract for bag-like objects (`ParameterBag`).

## Responsibilities

- Provide `ParameterBag`, a concurrency-safe map-backed key/value store.
- Offer typed value access helpers with predictable presence semantics.
- Provide “strict” variants that return a typed parse error when conversion is not possible.

## Value semantics

A bag entry has two separate properties:

- **Presence**: the key exists in the bag (the helper returns `exists == true`).
- **Value**: the raw stored value, which may be `nil`.

Important rules:

- Missing keys return `exists == false` for helpers that expose key presence (for example `String` and `StringStrict`).
- For conversion helpers (`Int`, `Bool`, `Float64`, `Duration`), the boolean result represents whether a typed value was present/produced (for example, it is `false` when the stored value is `nil`).
- When a key is present but the stored value cannot be converted, conversion helpers return an error (with the boolean typically `true`, meaning the key was present and conversion was attempted).
- `String` is intentionally permissive: it returns `""` for non-string stored types. Use `StringStrict` when you need validation and errors.

## Conversion semantics

The typed helpers distinguish three situations:

- **Key missing**: the bag does not contain the key. Helpers return `exists == false` and no error.
- **Key present, value is nil**: the bag contains the key, but its stored value is `nil`.
    - `String` / `StringStrict` treat this as present and return `""` with `exists == true`.
    - `Int` / `Bool` / `Float64` / `Duration` treat this as no typed value and return the zero value with `exists == false`.
- **Key present, value cannot be converted**: helpers return a typed parse error and `exists == true`.

Notes:

- **Empty string is a present value** for `String` / `StringStrict`.
- For non-string typed helpers, an empty string is parsed and may yield a conversion error (for example parsing `""` as an int).

## Usage

The example below demonstrates a typical Melody flow: build a parameter bag from an untyped source (`url.Values` from an HTTP request) and read values using typed helpers.

```go
package main

import (
	"net/url"
	"time"

	bagpackage "github.com/precision-soft/melody/bag"
)

func readRequestParameters(
	values url.Values,
) (string, int64, time.Duration, bool, error) {
	parameterBag := bagpackage.NewParameterBagFromValues(
		values,
	)

	name := bagpackage.StringOrDefault(
		parameterBag,
		"name",
		"anonymous",
	)

	count, countExists, countErr := bagpackage.Int(
		parameterBag,
		"count",
	)
	if nil != countErr {
		return "", 0, 0, false, countErr
	}
	if false == countExists {
		count = 0
	}

	timeout, timeoutExists, timeoutErr := bagpackage.Duration(
		parameterBag,
		"timeout",
	)
	if nil != timeoutErr {
		return "", 0, 0, false, timeoutErr
	}
	if false == timeoutExists {
		timeout = 0
	}

	enabled, enabledExists, enabledErr := bagpackage.Bool(
		parameterBag,
		"enabled",
	)
	if nil != enabledErr {
		return "", 0, 0, false, enabledErr
	}
	if false == enabledExists {
		enabled = false
	}

	return name, count, timeout, enabled, nil
}
```

## Footguns & caveats

- `String` returns `""` for non-string stored types; use `StringStrict` to detect type mismatches.
- `StringSlice` and `StringSliceStrict` accept both `[]string` and `string` (single value), returning a slice in both cases.
- `ParameterBag.All()` returns a copy of the internal map.

## Userland API

### Contracts (`bag/contract`)

#### Types

- **ParameterBag**  
  A map-like bag used across Melody for key/value parameter storage.

```go
package main

type ParameterBag interface {
	Set(name string, value any)

	Get(name string) (any, bool)

	Has(name string) bool

	Remove(name string)

	Count() int

	All() map[string]any
}
```

### Types

- **bag.ParameterBag**  
  Default concurrency-safe implementation of `bag/contract.ParameterBag`.

### Constructors

- `bag.NewParameterBag() *bag.ParameterBag`
- `bag.NewParameterBagFromValues(values url.Values) *bag.ParameterBag`

### Value helpers

#### Strings

- `bag.String(parameterBag, name) (string, bool)`
- `bag.StringOrDefault(parameterBag, name, defaultValue) string`
- `bag.HasNonEmptyString(parameterBag, name) bool`
- `bag.StringStrict(parameterBag, name) (string, bool, error)`

#### Numbers and booleans

- `bag.Int(parameterBag, name) (int64, bool, error)`
- `bag.Bool(parameterBag, name) (bool, bool, error)`
- `bag.Float64(parameterBag, name) (float64, bool, error)`

#### Durations

- `bag.Duration(parameterBag, name) (time.Duration, bool, error)`

#### Slices and maps

- `bag.StringSlice(parameterBag, name) ([]string, bool)`
- `bag.StringSliceStrict(parameterBag, name) ([]string, bool, error)`
- `bag.StringAt(parameterBag, name, index) (string, bool, error)`
- `bag.AppendString(parameterBag, name, value) error`
- `bag.AppendStringSlice(parameterBag, name, values) error`
- `bag.StringMapStringString(parameterBag, name) (map[string]string, bool, error)`
