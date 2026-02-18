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
- `MarkLogged` enables suppressing duplicate logs when errors cross layers.

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
