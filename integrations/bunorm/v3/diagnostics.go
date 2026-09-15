package bunorm

import (
    "io"
    "log"
    "os"
    "reflect"
    "sync"
    "sync/atomic"

    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/uptrace/bun"
)

const bunDiagnosticsMessage = "bun diagnostic"

var (

    bunDiagnosticsOnce sync.Once

    bunDiagnosticsTarget atomic.Pointer[diagnosticsTarget]
)

type diagnosticsTarget struct {
    logger loggingcontract.Logger
    writer io.Writer
}

/* RouteDiagnostics forwards bun diagnostics to the logger as warnings. The process-wide forwarder is installed once; routing replaces its atomic destination. Comparable logger identities reuse their writer, while non-comparable values receive a new destination on every call. Nil and typed-nil loggers leave the destination unchanged.

   This does not redirect the standard library logger used by the MySQL dialect's server-version diagnostic. */
func RouteDiagnostics(logger loggingcontract.Logger) {
    if nil == logger || true == isNilInterface(logger) {
        return
    }

    if owned, isOwned := logger.(*registryDiagnosticLogger); true == isOwned {
        owned.routingMutex.Lock()
        defer owned.routingMutex.Unlock()
        if true == owned.retired {
            return
        }
    }

    if live := bunDiagnosticsTarget.Load(); nil != live && sameDiagnosticLogger(logger, live.logger) {
        return
    }

    bunDiagnosticsTarget.Store(newDiagnosticsTarget(logger))

    bunDiagnosticsOnce.Do(installBunDiagnostics)
}

func sameDiagnosticLogger(left loggingcontract.Logger, right loggingcontract.Logger) bool {
    if nil == left || nil == right {
        return nil == left && nil == right
    }

    return reflect.ValueOf(left).Comparable() && reflect.ValueOf(right).Comparable() && left == right
}

type registryDiagnosticLogger struct {
    loggingcontract.Logger

    routingMutex sync.Mutex
    retired bool
}

func (instance *registryDiagnosticLogger) Enabled(level loggingcontract.Level) bool {
    reporter, reportsLevel := instance.Logger.(interface {
        Enabled(loggingcontract.Level) bool
    })
    if false == reportsLevel {
        return true
    }
    return reporter.Enabled(level)
}

func diagnosticLoggerWithIdentity(logger loggingcontract.Logger) loggingcontract.Logger {
    return &registryDiagnosticLogger{Logger: logger}
}

func retireRegistryDiagnostics(logger loggingcontract.Logger) {
    owned, isOwned := logger.(*registryDiagnosticLogger)
    if false == isOwned {
        resetDiagnosticsRoutedTo(logger)
        return
    }
    owned.routingMutex.Lock()
    defer owned.routingMutex.Unlock()
    owned.retired = true
    resetDiagnosticsRoutedTo(owned)
}

func newDiagnosticsTarget(logger loggingcontract.Logger) *diagnosticsTarget {
    return &diagnosticsTarget{
        logger: logger,
        writer: logging.NewStandardErrorLogger(logger, bunDiagnosticsMessage).Writer(),
    }
}

/* ResetDiagnostics unconditionally returns bun diagnostics to standard error. It is a no-op when routing is already reset. Registry teardown uses conditional ownership checks so closing one registry does not replace another registry’s destination. */
func ResetDiagnostics() {
    bunDiagnosticsTarget.Store(nil)
}

func resetDiagnosticsRoutedTo(logger loggingcontract.Logger) {
    live := bunDiagnosticsTarget.Load()
    if nil == live || false == sameDiagnosticLogger(logger, live.logger) {
        return
    }

    bunDiagnosticsTarget.CompareAndSwap(live, nil)
}

func installBunDiagnostics() {
    bun.SetLogger(log.New(&retargetableDiagnosticsWriter{}, "", 0))
}

type retargetableDiagnosticsWriter struct{}

/* Write hands the record to the live destination, or to standard error when there is none. The fallback is not a discard: an unrouted bun diagnostic belongs on standard error, because that is exactly where bun itself puts it, and swallowing it here would make ResetDiagnostics a silent mute rather than a hand-back. */
func (instance *retargetableDiagnosticsWriter) Write(data []byte) (int, error) {
    target := bunDiagnosticsTarget.Load()

    if nil == target || nil == target.writer {
        return os.Stderr.Write(data)
    }

    return target.writer.Write(data)
}

var _ io.Writer = (*retargetableDiagnosticsWriter)(nil)
