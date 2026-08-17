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
- Provide standard logger implementations through their constructors — `NewJsonLogger`, `NewDefaultLogger`, `NewNopLogger` — each returning `logging/contract.Logger`; the concrete types are unexported.
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

- `LogOnRecover` **never terminates the process.** It recovers the panic in flight, logs it once — an error already marked as logged is not logged again — and, when `panicAgain` is set, panics again: with the recovered value when it already is an `*exception.Error` or an `*exception.ExitError`, and with the exception built around it otherwise, so the mark that prevents a second record travels with what goes back up. A recovered `exception.ExitError` is logged like any other error and re-raised as the wrapper it was, not as the error it carries, so the exit code it holds survives for whoever owns the process boundary; one carrying no error value is logged as the anomaly it is. A foreign error or a plain value is wrapped together with the stack of the panic still in flight, under the `panicStack` context key. This is the helper a `defer` in a library-like position wants: installed with `defer`, it sits above every defer registered before it — the container teardown, the scope closes, the shutdown hooks — and an `os.Exit` from there would skip all of them. With `panicAgain` false it swallows the panic, which is a decision to make deliberately. See [`recover.go`](../../logging/recover.go).
- `LogOnRecoverAndExit` is the helper named for taking the exit, and the one to use only where the process boundary is yours. It takes the recovered value as an argument rather than calling `recover()` itself, logs it, and calls `os.Exit(...)` — with the code carried by `exception.ExitError` when the panic was one, with the `exitCode` it was given otherwise. The `exitCode` argument must sit in `[1, 255]`: anything else — zero included — is refused with a panic at the door, on every call, because `os.Exit` keeps only the low 8 bits and a code the boundary betrays would report success from a dying process. `LogOnRecoverAndExitAfter` additionally runs a hook between the record and the exit — the one place a teardown can sit without either being skipped by `os.Exit` or closing the very logger the final record needs. It writes **two** records, not one: the detailed one at the error's own level, which a configured threshold can silently discard, and an exit certificate at **emergency** level that no threshold can drop — the record that says the process is exiting and why, with the error carried in its context because the record's subject is the exit. Each step — the record, the certificate and the hook — runs under its own recover and its own 10-second budget: a panic in any of them costs one stderr line naming the step, and a step that overruns the budget is abandoned with a line of its own, never the exit code. Before a non-zero exit the helper echoes one line to stderr (`melody: exiting with code %d after unrecovered error: ...`), so a failing process is never completely silent on the standard streams even when the configured logger writes to a file; a zero exit code adds nothing. See [`recover.go`](../../logging/recover.go).
- `LogError` anchors the record on the error it is given: a top-level `*exception.Error` contributes its own level, message and context enriched with its cause chain, while any other error — wrappers included — is logged at error level under its full message, with the context of the nearest `ContextProvider` and the cause chain walked from its own wrap links. That walk is `errors.Unwrap`, which by its own contract does not open an `errors.Join`, so a **top-level** joined error contributes no branch at all — join a wrapped error rather than wrapping a join where the branches must reach the record. The already-logged mark is read at the depth `exception.MarkLogged` writes it — the nearest `AlreadyLogged` implementer — so marking a wrapping http exception suppresses the record here. See [`logger.go`](../../logging/logger.go).
- `NewRequestLogger` will not modify context if `requestId` is empty; it returns the base logger unchanged. The merged context carries the **real** request id under the context key unconditionally — a different non-empty value already sitting under the key is preserved beside it under the key suffixed `Claimed`, so client-derived data cannot forge the record's correlation. The decorator forwards `Closed()` to the base it wraps and deliberately does not forward `Close()` — it lives in a request scope and does not own the shared writer. See [`request_logger.go`](../../logging/request_logger.go).
- **`LevelReporter` is an optional capability, and its absence means "enabled".** A logger may implement [`loggingcontract.LevelReporter`](../../logging/contract/logger.go) (`Enabled(level Level) bool`) to say, before being handed anything, whether a record at that level would survive its threshold. It exists for the callers that *build* what they log — the event dispatcher assembles a context map per dispatch and resolves a listener's name through reflection per listener per dispatch, all of it discarded unread by a journal at error level. Ask it through [`logging.LevelEnabled(logger, level)`](../../logging/logger.go), never through your own type assertion: the fallback for a logger that does not implement it is **true**, and a site that spelled that the other way would stop recording against every logger that does not carry the capability. `jsonLogger` implements it against its own threshold (and reports nothing enabled once closed), `nopLogger` reports nothing enabled at all, and `requestLogger` forwards to the base it decorates — which is what makes it worth anything on the request path, since the decorator is what every handler and listener actually holds. Use it only where the answer saves work that is otherwise thrown away; a record whose arguments already exist costs nothing to hand over, and gating it would only add a second place where a level decision is made. `loggingcontract.Logger` is untouched, so an existing logger keeps working unchanged.
- Context keys should be camelCase. This is relied on across Melody (for example `processId`, `requestId`).
- **The file journal is one descriptor for the life of the process, and rename-based rotation loses it silently.** The default configuration logs to a file (`MELODY_LOG_PATH` defaults to `%kernel.logs_dir%/%kernel.environment%.log`), the container opens it exactly once when the logger is first resolved, and no reopen door exists: the process listens for no rotation signal (`SIGINT`/`SIGTERM` are the whole set) and nothing rebuilds the logger before teardown. A logrotate in its default rename-and-create mode therefore moves the journal out from under the process — every later record lands in the rotated file, or, once that is compressed or deleted, in an unlinked inode holding disk space that only `lsof` can still see — with no error anywhere, because writes to a renamed or deleted-but-open inode keep succeeding. Rotate this journal with **`copytruncate`**: the descriptor is opened with `O_APPEND`, whose per-write offset makes truncation-in-place safe, at the price of the small copy window that mode always carries. Reopening on a signal is a feature the stable majors do not grow; restart the process after any rename-mode rotation.
- [`NewJsonLogger`](../../logging/json_logger.go) serializes writes through an internal mutex so concurrent calls produce cleanly-separated JSON lines on the underlying writer, stamped with `RFC3339Nano` precision so the write order stays reconstructible. The stamp is taken **inside** that mutex, together with the encoding, which is what makes the order of the stamps the order of the writes: taken above the lock it said when the record was formed, and the encoding sitting between the stamp and the write put records on the file in an order the stamps did not describe. The cost is that the encoding is serialized across writers — a logger writes to one destination through one lock either way, and this is what the ordering above is worth. Only the normalization of the caller's own context stays outside the lock, because it walks nothing this logger shares. Errors in the context — top-level or nested inside the map and slice shapes the framework produces — render as their messages, except an error that also implements `json.Marshaler`, which is handed to the encoder for a structural rendering (`validation.ValidationErrors` opts in, so the log says what the response body says); a record whose context cannot be marshaled falls back to a line that keeps the context as rendered text beside the marshal error. A record whose level is not one of the five known ones is weighed at error priority — the unknown level is the case that least deserves silence — with the raw level as its label. `Close` leaves the process console open, recognized by identity (`os.Stdout`/`os.Stderr` themselves) — a file the application opened on a console-looking path is really closed; `Closed()` answers lock-free, so the process-boundary exit handler is never queued behind a stalled write. The level-label map is copied at construction, so a reference the caller keeps cannot race the lock-free label reads. A failed write — a full disk, a read-only mount, a vanished volume — is echoed once for the life of the logger on stderr, written there directly rather than through `EmergencyLogger`, which is itself a json logger and would re-enter the write that just failed; the echo is skipped when the logger's own output already is stderr. Once, because a logger writing into a full disk fails on every record it is handed.

## Userland API

### Contracts (`logging/contract`)

Implemented in:

- [`./logging/contract/logger.go`](../../logging/contract/logger.go)
- [`./logging/contract/level.go`](../../logging/contract/level.go)
- [`./logging/contract/config.go`](../../logging/contract/config.go)

#### Types

- **Logger**
- **LevelReporter** — the optional capability a `Logger` may implement to answer whether a level is enabled; ask through [`logging.LevelEnabled`](../../logging/logger.go), whose answer for a logger without it is `true`
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

### Implementations and helpers (`logging`)

#### Constructors

- [`NewJsonLogger(output io.Writer, minLevel loggingcontract.Level)`](../../logging/json_logger.go)
- [`NewJsonLoggerWithLabels(output io.Writer, minLevel loggingcontract.Level, labels loggingcontract.LevelLabels)`](../../logging/json_logger.go)
- [`NewDefaultLogger()`](../../logging/default_logger.go)
- [`NewDefaultLoggerWithLabels(labels loggingcontract.LevelLabels)`](../../logging/default_logger.go)
- [`NewNopLogger()`](../../logging/nop_logger.go)
- [`NewRequestLogger(logger loggingcontract.Logger, requestId string, contextKey string)`](../../logging/request_logger.go) — the http path's correlation rule: the real id wins the context key, and a different non-empty string claim survives beside it under the key suffixed `Claimed`
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
