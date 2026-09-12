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

/* bunDiagnosticsMessage is the one groupable message every routed bun diagnostic is filed under; the line itself travels in the record's context. */
const bunDiagnosticsMessage = "bun diagnostic"

var (
    /* the setting itself happens once, because bun's logger is one variable for the whole process and re-setting it would race every goroutine reading it. What the once installs is a FORWARDER, not a destination, so the once no longer decides where the records go. */
    bunDiagnosticsOnce sync.Once
    /* the live destination, replaceable for the life of the process. A nil pointer means no destination, and the forwarder falls back to standard error — where bun writes when nobody routes it at all. */
    bunDiagnosticsTarget atomic.Pointer[diagnosticsTarget]
)

/* diagnosticsTarget is one installed writer and the logger identity used for conditional teardown. */
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

/* A value logger with non-comparable fields needs a stable registry-owned identity for routing and teardown. */
type registryDiagnosticLogger struct {
    loggingcontract.Logger
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
    if reflect.ValueOf(logger).Comparable() {
        return logger
    }

    return &registryDiagnosticLogger{Logger: logger}
}

/* newDiagnosticsTarget builds the destination one routing installs. The writer is built once per routing rather than per record, and it is logging.NewStandardErrorLogger's own, so the record shape stays the framework's and is not spelled a second time here. */
func newDiagnosticsTarget(logger loggingcontract.Logger) *diagnosticsTarget {
    return &diagnosticsTarget{
        logger: logger,
        writer: logging.NewStandardErrorLogger(logger, bunDiagnosticsMessage).Writer(),
    }
}

/* ResetDiagnostics hands bun's diagnostic channel back: the records go to standard error again, which is where bun writes them when no one routes them at all. It is what a teardown calls while the logger it routed to is still alive, so nothing is written into a journal that is closing — the ManagerRegistry calls it from its own Close for exactly that reason, and the container's teardown order puts the registry ahead of the logging service because the registry resolves it.

   It is also the door for a process that hosts melody rather than being one: a binary with its own reporting for bun takes the channel back with this and sets its own logger afterwards. Calling it when nothing was ever routed is not an error and changes nothing. It hands the channel back whoever routed it; the registry's own teardown goes through resetDiagnosticsRoutedTo instead, so that one registry closing does not take the channel away from another. */
func ResetDiagnostics() {
    bunDiagnosticsTarget.Store(nil)
}

/* resetDiagnosticsRoutedTo releases only the matching destination. Registries wrap non-comparable loggers with a stable identity; a direct non-comparable value cannot prove ownership and must use ResetDiagnostics for an unconditional hand-back. */
func resetDiagnosticsRoutedTo(logger loggingcontract.Logger) {
    live := bunDiagnosticsTarget.Load()
    if nil == live || false == sameDiagnosticLogger(logger, live.logger) {
        return
    }

    bunDiagnosticsTarget.CompareAndSwap(live, nil)
}

/* installBunDiagnostics performs the setting itself, apart from the once that guards it, so the destination can be proven without the guard standing in the way of a second proof. The once is never reset and never needs to be: what it installed is the forwarder, which reads the live destination at every record, so a routing after a hand-back reaches the journal again through the same forwarder — measured, a record after ResetDiagnostics and a fresh RouteDiagnostics arrives at the fresh logger. */
func installBunDiagnostics() {
    bun.SetLogger(log.New(&retargetableDiagnosticsWriter{}, "", 0))
}

/* retargetableDiagnosticsWriter is the one writer bun ever holds. It carries no destination of its own — it reads the live one at every record, which is what lets a later lifecycle replace it without touching bun's package-level variable a second time. */
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
