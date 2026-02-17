# LOGGING

The [`logging`](../../logging) package provides Melody’s structured logging primitives and integration helpers for container/runtime access.

## Scope

- Package: [`logging/`](../../logging)
- Subpackage: [`logging/contract/`](../../logging/contract)

## Subpackages

- [`logging/contract`](../../logging/contract)  
  Public contracts for logging (`Logger`, `Level`, `Context`).

## Responsibilities

- Define the `logging/contract.Logger` interface and the `logging/contract.Level` levels.
- Provide standard logger implementations (`JsonLogger`, `DefaultLogger`, `NopLogger`).
- Provide request/process-scoped logger decoration via `NewRequestLogger`.
- Provide panic/exit recovery helpers that log Melody exceptions (`LogOnRecover`, `LogOnRecoverAndExit`).
- Provide container/runtime helpers for resolving a logger from Melody’s DI container/runtime.

## Container and runtime integration

The package defines the logger service id:

- [`ServiceLogger`](../../logging/service_resolver.go) (`"service.logger"`)

Resolution helpers:

- [`LoggerMustFromContainer`](../../logging/service_resolver.go)
- [`LoggerFromContainer`](../../logging/service_resolver.go)
- [`LoggerMustFromRuntime`](../../logging/service_resolver.go)
- [`LoggerFromRuntime`](../../logging/service_resolver.go)

## Usage

The example below demonstrates a typical Melody flow:

- resolve the logger from the container;
- create a process-scoped logger;
- override the protected logger service inside a request scope.

```go
package main

import (
	"context"

	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/logging"
	"github.com/precision-soft/melody/runtime"
)

func runWithScopedLogger(
	ctx context.Context,
	serviceContainer containercontract.Container,
) {
	baseLogger := logging.LoggerMustFromContainer(
		serviceContainer,
	)

	scope := serviceContainer.NewScope()
	defer func() {
		scopeCloseErr := scope.Close()
		if nil != scopeCloseErr {
			logging.EmergencyLogger().Error("failed to close scope", map[string]any{"error": scopeCloseErr.Error()})
		}
	}()

	runtimeInstance := runtime.New(
		ctx,
		scope,
		serviceContainer,
	)

	processId := logging.GenerateProcessId()
	loggerWithProcessId := logging.NewRequestLogger(
		baseLogger,
		processId,
		"processId",
	)

	scope.MustOverrideProtectedInstance(
		logging.ServiceLogger,
		loggerWithProcessId,
	)

	resolvedLogger := logging.LoggerMustFromRuntime(
		runtimeInstance,
	)
	_ = resolvedLogger
}
```

## Footguns & caveats

- `LogOnRecover` / `LogOnRecoverAndExit` will treat Melody’s `exception.ExitError` specially and terminate the process via `os.Exit(...)`. See [`recover.go`](../../logging/recover.go).
- `NewRequestLogger` will not modify context if `requestId` is empty; it returns the base logger unchanged. See [`request_logger.go`](../../logging/request_logger.go).
- Context keys should be camelCase. This is relied on across Melody (for example `processId`, `requestId`).

## Userland API

### Contracts (`logging/contract`)

Implemented in:

- [`./logging/contract/logger.go`](../../logging/contract/logger.go)
- [`./logging/contract/level.go`](../../logging/contract/level.go)

#### Types

- **Logger**
- **Level**
- **Context**

#### Levels

- `LevelDebug`, `LevelInfo`, `LevelWarning`, `LevelError`, `LevelEmergency`

### Implementations and helpers (`logging`)

#### Constructors

- [`NewJsonLogger(output io.Writer, minLevel loggingcontract.Level)`](../../logging/json_logger.go)
- [`NewDefaultLogger()`](../../logging/default_logger.go)
- [`NewNopLogger()`](../../logging/nop_logger.go)
- [`NewRequestLogger(logger loggingcontract.Logger, requestId string, contextKey string)`](../../logging/request_logger.go)

#### Utilities

- [`GenerateProcessId()`](../../logging/process_id.go)
- [`EnsureLogger(logger loggingcontract.Logger)`](../../logging/nop_logger.go)
- [`IsValidLevel(value loggingcontract.Level)`](../../logging/logger.go)

#### Recovery

- [`LogOnRecover(logger loggingcontract.Logger, panicAgain bool)`](../../logging/recover.go)
- [`LogOnRecoverAndExit(logger loggingcontract.Logger, exitCode int)`](../../logging/recover.go)

#### Emergency logger

- [`EmergencyLogger()`](../../logging/emergency_logger.go)
- [`CloseEmergencyLogger()`](../../logging/emergency_logger.go)

#### Container/runtime access

- [`const ServiceLogger`](../../logging/service_resolver.go)
- [`LoggerMustFromRuntime(runtimeInstance runtimecontract.Runtime)`](../../logging/service_resolver.go)
- [`LoggerFromRuntime(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger`](../../logging/service_resolver.go)
- [`LoggerMustFromContainer(serviceContainer containercontract.Container)`](../../logging/service_resolver.go)
- [`LoggerFromContainer(serviceContainer containercontract.Container) (loggingcontract.Logger, error)`](../../logging/service_resolver.go)
