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
    - [`Logged`](../../exception/utility.go)
    - [`IsAlreadyLogged`](../../exception/utility.go)
    - [`PanicCause`](../../exception/utility.go)
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
	configcontract "github.com/precision-soft/melody/config/contract"
	"github.com/precision-soft/melody/exception"
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

	"github.com/precision-soft/melody/exception"
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
- `MarkLogged` enables suppressing duplicate logs when errors cross layers. It marks the nearest markable error in the chain — the same depth at which `logging.LogError` reads the mark back — so marking a wrapped error is not a no-op.
- `MarkLogged` has nowhere to write on an error whose chain carries no markable link: `errors.New`, `fmt.Errorf` and every runtime error implement neither exception type, so the call is a silent no-op and the next reader files the same failure again. `Logged` is what a writer returns after filing its record — it marks in place when it can, keeping the error's identity and every `errors.Is`/`errors.As` its readers perform, and otherwise hands back a marked melody error keeping the original as its cause. The wrap cannot change a resolved status: it happens exactly when no `HttpException` is in the chain, which is exactly when the status was already the generic one.
- A recovery boundary that fabricates an error in place of a panic passes the recovered value through [`PanicCause`](../../exception/utility.go) into the CAUSE slot, and captures `debug.Stack()` into the context under `panicStack`. An error-shaped panic value kept only in a context slot collapses to its bare message — the json logger stringifies an error it finds in a context — so the context map and the cause chain of the very error that was raised reach no record at all, and the reason a write failed is gone while the stack that says where survives. `PanicCause` answers nothing for a typed nil, whose `Error()` would dereference a nil receiver at the first render of the record the boundary exists to write, and nothing for a panic value that is not an error, which the boundary still renders into its context.
- Every utility treats a typed nil — a non-nil `error` interface holding a nil concrete pointer — as the nil it means: `LogContext`, `FromError` and its variants, `MarkLogged` and both cause-chain builders neither panic on one nor hand one onward.
- The value ranges are enforced at construction: `NewExitError` refuses an exit code outside `[1, 255]` (`os.Exit` keeps only the low 8 bits, so 256 reports success from a dying process), and `NewHttpException`/`NewHttpExceptionWithCause` refuse a status code outside `[100, 599]` (`net/http`'s `WriteHeader` panics outside `[100, 999]`, and a clamped status serves an exception as success). Both refusals are panics.
- `ValidationFailed` stores its detail under the `errors` context key, the one the kernel exception listener copies into the json error payload, with status 422.
- The log record built by `LogContext` takes `cause`/`causeChain`/`causeContextChain` from the top error's own wrap links — links, plural, because a joined error has several, and each branch contributes side by side — one entry per link; any link implementing `ContextProvider` contributes its context. Every extra context map passed to it is merged in order, later entries winning.
- Both exception types guard their context map and already-logged mark internally, because a memoized failure (a container creation error, a container close error) is reachable from several request goroutines at once. Error handling otherwise stays single-threaded per request by design.

## Userland API

### Contracts (`exception/contract`)

- [`type Context`](../../exception/contract/context.go)
- [`type ContextProvider`](../../exception/contract/context.go)
- [`type AlreadyLogged`](../../exception/contract/already_logged.go)

### Constructors and utilities (`exception`)

- Error constructors:
    - [`NewError`](../../exception/error_new.go)
    - [`NewWarning`](../../exception/error_new.go)
    - [`NewInfo`](../../exception/error_new.go)
    - [`NewEmergency`](../../exception/error_new.go)
- Error utilities:
    - [`LogContext`](../../exception/utility.go)
    - [`FromError`](../../exception/utility.go)
    - [`FromErrorWithLevel`](../../exception/utility.go)
    - [`FromErrorWithLevelAndContext`](../../exception/utility.go)
    - [`MarkLogged`](../../exception/utility.go)
    - [`Logged(err error) error`](../../exception/utility.go)
    - [`IsAlreadyLogged(err error) bool`](../../exception/utility.go)
    - [`PanicCause(recoveredValue any) error`](../../exception/utility.go)
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
