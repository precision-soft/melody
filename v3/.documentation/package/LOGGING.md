# LOGGING

The [`logging`](../../logging) package provides Melody’s structured logging primitives and integration helpers for container/runtime access.

## Scope

- Package: [`logging/`](../../logging)
- Subpackage: [`logging/contract/`](../../logging/contract)

## Subpackages

- [`logging/contract`](../../logging/contract)
  Public contracts for logging (`Logger`, `Level`, `Context`, `LevelLabels`, `LoggingConfiguration`).

## Responsibilities

- Define the `logging/contract.Logger` interface and the `logging/contract.Level` levels.
- Provide standard logger implementations (`JsonLogger`, `DefaultLogger`, `NopLogger`).
- Allow application-level customization of log-level output labels via `LoggingConfiguration` and `ConfigModule`.
- Provide request/process-scoped logger decoration via `NewRequestLogger`.
- Provide panic/exit recovery helpers that log Melody exceptions (`LogOnRecover`, `LogOnRecoverAndExit`).
- Provide container/runtime helpers for resolving a logger from Melody’s DI container/runtime.

## Configuration

The log-level label output is customizable at application level via [`LoggingConfiguration`](../../logging/contract/config.go).

By default all levels use their string names (`"debug"`, `"info"`, `"warning"`, `"error"`, `"emergency"`). To override, register a `LoggingConfiguration` from a [`ConfigModule`](../../application/contract/config_module.go):

```go
func (instance *appModule) RegisterConfigurations(registrar applicationcontract.ConfigRegistrar) {
    registrar.RegisterConfiguration(
        loggingcontract.LoggingConfigurationName,
        logging.NewLoggingConfiguration(loggingcontract.LevelLabels{
            loggingcontract.LevelDebug:     loggingcontract.LevelLabelFromInt(100),
            loggingcontract.LevelInfo:      loggingcontract.LevelLabelFromInt(200),
            loggingcontract.LevelWarning:   loggingcontract.LevelLabelFromInt(300),
            loggingcontract.LevelError:     loggingcontract.LevelLabelFromInt(400),
            loggingcontract.LevelEmergency: loggingcontract.LevelLabelFromInt(500),
        }),
    )
}
```

Any level absent from the map falls back to its `Level` string value.

## Container and runtime integration

The package defines the logger service id:

- [`ServiceLogger`](../../logging/service_resolver.go) (`"service.logger"`)

Resolution helpers:

- [`LoggerMustFromContainer`](../../logging/service_resolver.go)
- [`LoggerFromContainer`](../../logging/service_resolver.go)
- [`LoggerMustFromResolver`](../../logging/service_resolver.go)
- [`LoggerFromResolver`](../../logging/service_resolver.go)
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

	containercontract "github.com/precision-soft/melody/v3/container/contract"
	"github.com/precision-soft/melody/v3/logging"
	"github.com/precision-soft/melody/v3/runtime"
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
	loggerWithProcessId := logging.NewProcessLogger(
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

- `LogOnRecover` **never terminates the process.** It recovers the panic in flight, logs it once — an `*exception.Error` already marked as logged is not logged again — and, when `panicAgain` is set, panics again with the same value. A recovered `exception.ExitError` is logged like any other error and re-raised as the wrapper it was, not as the error it carries, so the exit code it holds survives for whoever owns the process boundary. This is the helper a `defer` in a library-like position wants: installed with `defer`, it sits above every defer registered before it — the container teardown, the scope closes, the shutdown hooks — and an `os.Exit` from there would skip all of them. With `panicAgain` false it swallows the panic, which is a decision to make deliberately. See [`recover.go`](../../logging/recover.go).
- `LogOnRecoverAndExit` is the helper named for taking the exit, and the one to use only where the process boundary is yours. It takes the recovered value as an argument rather than calling `recover()` itself, logs it, and calls `os.Exit(...)` — with the code carried by `exception.ExitError` when the panic was one, with the `exitCode` it was given otherwise. Before a non-zero exit it echoes one line to stderr (`melody: exiting with code %d after unrecovered error: ...`), so a failing process is never completely silent on the standard streams even when the configured logger writes to a file; a zero exit code adds nothing. See [`recover.go`](../../logging/recover.go).
- `NewRequestLogger` will not modify context if `requestId` is empty; it returns the base logger unchanged. See [`request_logger.go`](../../logging/request_logger.go).
- Context keys should be camelCase. This is relied on across Melody (for example `processId`, `requestId`).
- [`NewJsonLogger`](../../logging/json_logger.go) serializes writes through an internal mutex so concurrent calls produce cleanly-separated JSON lines on the underlying writer.

## Userland API

### Contracts (`logging/contract`)

Implemented in:

- [`./logging/contract/logger.go`](../../logging/contract/logger.go)
- [`./logging/contract/level.go`](../../logging/contract/level.go)
- [`./logging/contract/config.go`](../../logging/contract/config.go)

#### Types

- **Logger**
- **Level**
- **Context**
- **LevelLabel** — wraps a label value (string or int); use `LevelLabelFromString` / `LevelLabelFromInt` to construct
- **LevelLabels** — maps each `Level` to a `LevelLabel`
- **LoggingConfiguration** — holds the application-level logging module configuration

#### Levels

- `LevelDebug`, `LevelInfo`, `LevelWarning`, `LevelError`, `LevelEmergency`

#### Level labels

- [`LevelLabelFromString(s string)`](../../logging/contract/level.go) — constructs a `LevelLabel` from a string value
- [`LevelLabelFromInt(i int)`](../../logging/contract/level.go) — constructs a `LevelLabel` from an int value
- [`(LevelLabel).String()`](../../logging/contract/level.go) — returns the label as a string
- [`DefaultLevelLabels()`](../../logging/contract/level.go) — returns the default `LevelLabels` map (`"debug"`, `"info"`, etc.)
- [`(LevelLabels).LabelFor(level)`](../../logging/contract/level.go) — returns the label string for a level, falling back to the `Level` string value

#### Logging configuration

- [`const LoggingConfigurationName`](../../logging/contract/config.go) — registry key (`"logging"`)
- [`LoggingConfigurationFromModules(moduleConfigurations map[string]any)`](../../logging/logging_config.go) — what turns the `ConfigModule` registration described above into the logger's labels. A nil map answers the defaults, and a registration under the right name holding the wrong type panics rather than falling back

### Implementations and helpers (`logging`)

#### Constructors

- [`NewJsonLogger(output io.Writer, minLevel loggingcontract.Level)`](../../logging/json_logger.go)
- [`NewJsonLoggerWithLabels(output io.Writer, minLevel loggingcontract.Level, labels loggingcontract.LevelLabels)`](../../logging/json_logger.go)
- [`NewDefaultLogger()`](../../logging/default_logger.go)
- [`NewDefaultLoggerWithLabels(labels loggingcontract.LevelLabels)`](../../logging/default_logger.go)
- [`NewNopLogger()`](../../logging/nop_logger.go)
- [`NewRequestLogger(logger loggingcontract.Logger, requestId string, contextKey string)`](../../logging/request_logger.go)
- [`NewProcessLogger(logger loggingcontract.Logger, processId string, contextKey string)`](../../logging/request_logger.go) — the console path's correlation rule, for a trusted caller: the generated id wins the context key, and a caller's own value — any type — is preserved verbatim under the key suffixed `Provided`
- [`NewStandardErrorLogger(logger loggingcontract.Logger, message string) *log.Logger`](../../logging/standard_logger.go) — the adapter net/http's `Server.ErrorLog` wants: one record per line at warning, the line carried in the context under `line`, the message left as the one groupable string a query can key on. A nil logger is inert and an empty line writes nothing
- [`NewLoggingConfiguration(labels loggingcontract.LevelLabels)`](../../logging/logging_config.go)

#### Utilities

- [`GenerateProcessId()`](../../logging/process_id.go)
- [`EnsureLogger(logger loggingcontract.Logger)`](../../logging/nop_logger.go)
- [`IsValidLevel(value loggingcontract.Level)`](../../logging/logger.go)
- [`LogError(logger loggingcontract.Logger, err error)`](../../logging/logger.go)

#### Recovery

- [`LogOnRecover(logger loggingcontract.Logger, panicAgain bool)`](../../logging/recover.go)
- [`LogOnRecoverAndExit(logger loggingcontract.Logger, recovered any, exitCode int)`](../../logging/recover.go)
- [`LogOnRecoverAndExitAfter(logger loggingcontract.Logger, recovered any, exitCode int, beforeExit func())`](../../logging/recover.go)
- [`RunShieldedStep(stepName string, step func()) bool`](../../logging/recover.go) — the exit handler's own shield, offered to the one other caller that stands between a process and its end: it contains a panic inside the step and bounds how long the step may take, answering whether it finished. An abandoned step keeps running on its goroutine, so nothing it writes may be read by a caller that was told it did not finish

#### Emergency logger

- [`EmergencyLogger()`](../../logging/emergency_logger.go)
- [`CloseEmergencyLogger()`](../../logging/emergency_logger.go)

#### Container/runtime access

- [`const ServiceLogger`](../../logging/service_resolver.go)
- [`LoggerMustFromRuntime(runtimeInstance runtimecontract.Runtime)`](../../logging/service_resolver.go)
- [`LoggerFromRuntime(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger`](../../logging/service_resolver.go)
- [`LoggerMustFromContainer(serviceContainer containercontract.Container)`](../../logging/service_resolver.go)
- [`LoggerFromContainer(serviceContainer containercontract.Container) (loggingcontract.Logger, error)`](../../logging/service_resolver.go)
- [`LoggerMustFromResolver(resolver containercontract.Resolver)`](../../logging/service_resolver.go)
- [`LoggerFromResolver(resolver containercontract.Resolver) (loggingcontract.Logger, error)`](../../logging/service_resolver.go)
