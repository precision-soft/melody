# EXCEPTION

The [`exception`](../../exception) package provides Melody’s fail-fast and structured error primitives. It standardizes error construction with contextual metadata, supports HTTP-aware exceptions, and provides a single place to convert fatal conditions into panics or process exits.

## Scope

Melody uses this package to:

- construct errors with structured context (`exception.NewError`, `exception.NewWarning`, …),
- attach and extract loggable context from arbitrary errors,
- mark errors as already logged to avoid duplicate logging,
- represent HTTP errors (`HttpException`) in the HTTP stack,
- enforce fail-fast behavior (`exception.Panic`) instead of raw `panic`.

## Subpackages

- [`exception/contract`](../../exception/contract)  
  Shared contracts (`Context`, `ContextProvider`, `AlreadyLogged`).

## Responsibilities

- Provide the structured error type itself:
    - [`Error`](../../exception/error.go) — message + loggable context + wrapped cause + log level
- Error constructors and error utilities:
    - [`NewError`](../../exception/error_new.go)
    - [`NewWarning`](../../exception/error_new.go)
    - [`NewInfo`](../../exception/error_new.go)
    - [`NewEmergency`](../../exception/error_new.go)
    - [`FromError`](../../exception/utility.go)
    - [`FromErrorWithLevel`](../../exception/utility.go)
    - [`FromErrorWithLevelAndContext`](../../exception/utility.go)
    - [`LogContext`](../../exception/utility.go)
    - [`MarkLogged`](../../exception/utility.go)
- Fail-fast helpers:
    - [`Panic`](../../exception/panic.go)
    - [`Exit`](../../exception/panic.go)
    - [`ExitError`](../../exception/exit.go)
    - [`NewExitError`](../../exception/exit.go)
- HTTP exception helpers:
    - [`HttpException`](../../exception/http_exception.go)
    - [`IsHttpException`](../../exception/http_exception.go)
    - [`AsHttpException`](../../exception/http_exception.go)
    - [`ValidationFailed`](../../exception/http_exception.go)
    - HTTP exception constructors (status helpers) in [`http_exception_new.go`](../../exception/http_exception_new.go)

## Usage

### Fail-fast on missing configuration

```go
package main

import (
	configcontract "github.com/precision-soft/melody/v3/config/contract"
	"github.com/precision-soft/melody/v3/exception"
)

func requireHttpAddress(configuration configcontract.Configuration) string {
	address := configuration.Http().Address()
	if "" == address {
		exception.Panic(
			exception.NewError(
				"missing http address",
				map[string]any{
					"parameter": "http.address",
				},
				nil,
			),
		)
	}

	return address
}
```

### Wrap an underlying error with context

```go
package main

import (
	"os"

	"github.com/precision-soft/melody/v3/exception"
)

func readFile(path string) []byte {
	data, readErr := os.ReadFile(path)
	if nil != readErr {
		exception.Panic(
			exception.NewError(
				"failed to read file",
				map[string]any{
					"path": path,
				},
				readErr,
			),
		)
	}

	return data
}
```

## Footguns & caveats

- `exception.Panic` is the framework-standard fail-fast mechanism; it intentionally does not attempt to recover.
- Context keys should use camelCase (for example, `"serviceName"`, `"httpStatusCode"`), consistent with framework conventions.
- `MarkLogged` enables suppressing duplicate logs when errors cross layers.

## Userland API

### Contracts (`exception/contract`)

- [`type Context`](../../exception/contract/context.go)
- [`type ContextProvider`](../../exception/contract/context.go)
- [`type AlreadyLogged`](../../exception/contract/already_logged.go)

### The error type (`exception`)

- [`type Error`](../../exception/error.go) — the structured error every constructor below returns. It carries a message, a loggable context, a wrapped cause and a log level, and it satisfies both [`exceptioncontract.ContextProvider`](../../exception/contract/context.go) and [`exceptioncontract.AlreadyLogged`](../../exception/contract/already_logged.go). All fields are unexported; the method set is the whole surface:
    - `Error() string` / `Message() string` — the message. Both return the same value, so `Error` reads the message alone and never the cause chain.
    - `Unwrap() error` / `CauseErr() error` — the wrapped cause, so `errors.Is` and `errors.As` traverse it.
    - `Context() exceptioncontract.Context` — a **copy** of the context, so a caller cannot mutate the error's own map.
    - `SetContext(context exceptioncontract.Context)` — replaces the context with a copy of the argument.
    - `SetContextValue(key string, value any)` — sets one key in place, for enriching an error as it crosses a layer.
    - `Level() loggingcontract.Level` — the level chosen by the constructor, which is what the logger records the error at.
    - `AlreadyLogged() bool` / `MarkAsLogged()` — the duplicate-log guard; [`MarkLogged`](../../exception/utility.go) is the free-function form that also handles a non-`*Error`.

  `Panic` takes an `*Error` rather than a plain `error`, so a foreign error is converted with [`FromError`](../../exception/utility.go) first.

### Constructors and utilities (`exception`)

- Error constructors — all four share the signature `(message string, context exceptioncontract.Context, causeErr error) *Error` and differ only in the level they stamp:
    - [`NewEmergency`](../../exception/error_new.go) — `LevelEmergency`
    - [`NewError`](../../exception/error_new.go) — `LevelError`
    - [`NewWarning`](../../exception/error_new.go) — `LevelWarning`
    - [`NewInfo`](../../exception/error_new.go) — `LevelInfo`
- Error utilities:
    - [`LogContext`](../../exception/utility.go)
    - [`BuildCauseChain(causeErr error, maxDepth int) []string`](../../exception/utility.go) — the `causeChain` value a log record carries: every wrap link rendered as text, breadth-first and bounded by the depth, with a joined error contributing each of its branches
    - [`BuildCauseContextChain(causeErr error, maxDepth int) []map[string]any`](../../exception/utility.go) — the `causeContextChain` counterpart, one context map per link that implements `ContextProvider`
    - [`FromError`](../../exception/utility.go)
    - [`FromErrorWithLevel`](../../exception/utility.go)
    - [`FromErrorWithLevelAndContext`](../../exception/utility.go)
    - [`MarkLogged`](../../exception/utility.go)
- Fail-fast and exit:
    - [`Panic(err *Error)`](../../exception/panic.go)
    - [`Exit(err *ExitError)`](../../exception/panic.go)
    - [`type ExitError`](../../exception/exit.go)
    - [`NewExitError(exitCode int, err *Error) *ExitError`](../../exception/exit.go)

### HTTP exceptions (`exception`)

- [`type HttpException`](../../exception/http_exception.go)
- [`IsHttpException(error) bool`](../../exception/http_exception.go)
- [`AsHttpException(error) *HttpException`](../../exception/http_exception.go)
- [`ValidationFailed(validationErrors any) *HttpException`](../../exception/http_exception.go)
- Status constructors (for example, `BadRequest`, `NotFound`, `InternalServerError`) in [`http_exception_new.go`](../../exception/http_exception_new.go)
