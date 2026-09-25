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
    /* set once, because bun's logger is one variable for the whole process and re-setting it would race every goroutine reading it; what is set is a forwarder, so the destination stays replaceable */
    bunDiagnosticsOnce sync.Once
    /* the live destination, replaceable for the life of the process. A nil pointer means no destination, and the forwarder falls back to standard error — where bun writes when nobody routes it at all. */
    bunDiagnosticsTarget atomic.Pointer[diagnosticsTarget]
)

/* diagnosticsTarget holds the writer one routing installed, built through logging.NewStandardErrorLogger, and the logger it was built for, so a repeated routing on the same logger installs nothing and a teardown hands the channel back only when it is its own. */
type diagnosticsTarget struct {
    logger loggingcontract.Logger
    writer io.Writer
}

/* RouteDiagnostics sends bun's own diagnostic channel, which reports declaration mistakes such as an unknown struct tag option, to the application's journal as warning records. Bun's logger is set once for the process, to a forwarder whose destination each routing on a different logger replaces, so a rebuilt application takes the channel back; a routing on the logger already installed changes nothing, and a nil or typed-nil logger routes nothing. The mysql dialect's server-version line goes through the standard library's default logger and is not reached; see the mysql readme. */
func RouteDiagnostics(logger loggingcontract.Logger) {
    routeDiagnosticsTo(logger)
}

/* routeDiagnosticsTo is RouteDiagnostics answering the destination it left live, so a registry can hand exactly that one back at its Close. A logger whose dynamic type carries no identity is routed afresh on every call; nil and a typed nil route nothing and answer nil. */
func routeDiagnosticsTo(logger loggingcontract.Logger) *diagnosticsTarget {
    if true == isNilInterface(logger) {
        return nil
    }

    if live := bunDiagnosticsTarget.Load(); nil != live && true == isSameLogger(logger, live.logger) {
        return live
    }

    target := newDiagnosticsTarget(logger)

    bunDiagnosticsTarget.Store(target)

    bunDiagnosticsOnce.Do(installBunDiagnostics)

    return target
}

/* newDiagnosticsTarget builds the destination one routing installs, around logging.NewStandardErrorLogger's writer, so the record shape stays the framework's. */
func newDiagnosticsTarget(logger loggingcontract.Logger) *diagnosticsTarget {
    return &diagnosticsTarget{
        logger: logger,
        writer: logging.NewStandardErrorLogger(logger, bunDiagnosticsMessage).Writer(),
    }
}

/* ResetDiagnostics hands bun's diagnostic channel back to standard error, whoever routed it, so a teardown writes nothing into a journal that is closing and a process hosting melody can take the channel for its own logger. Calling it when nothing is routed changes nothing; a registry's own teardown goes through resetDiagnosticsRoutedTo instead. */
func ResetDiagnostics() {
    bunDiagnosticsTarget.Store(nil)
}

/* resetDiagnosticsRoutedTo hands the channel back only when the live destination is the one routed to this logger, or the very destination the caller routed, so one registry closing does not take the channel from another; two registries on one logger share one channel. The destination is compared by identity, since a logger holding a slice, a map or a func has none and reading it by content would race its Log. */
func resetDiagnosticsRoutedTo(logger loggingcontract.Logger, routed *diagnosticsTarget) {
    live := bunDiagnosticsTarget.Load()
    if nil == live {
        return
    }

    if live != routed && false == isSameLogger(logger, live.logger) {
        return
    }

    bunDiagnosticsTarget.CompareAndSwap(live, nil)
}

/* isSameLogger answers whether two loggers are one value without the panic of comparing two interfaces whose dynamic type is not comparable. Such a value has no identity, so two of them are never the same. */
func isSameLogger(left loggingcontract.Logger, right loggingcontract.Logger) bool {
    if nil == left || nil == right {
        return nil == left && nil == right
    }

    if false == reflect.ValueOf(left).Comparable() || false == reflect.ValueOf(right).Comparable() {
        return false
    }

    return left == right
}

/* installBunDiagnostics performs the setting apart from the once that guards it, so a test can prove it. The once is never reset: the forwarder reads the live destination at every record, so a routing after a hand-back reaches the journal again. */
func installBunDiagnostics() {
    bun.SetLogger(log.New(&retargetableDiagnosticsWriter{}, "", 0))
}

/* retargetableDiagnosticsWriter is the one writer bun ever holds. It carries no destination of its own — it reads the live one at every record, which is what lets a later lifecycle replace it without touching bun's package-level variable a second time. */
type retargetableDiagnosticsWriter struct{}

/* Write hands the record to the live destination, or to standard error when there is none, where bun itself writes an unrouted diagnostic, so ResetDiagnostics is a hand-back rather than a mute. */
func (instance *retargetableDiagnosticsWriter) Write(data []byte) (int, error) {
    target := bunDiagnosticsTarget.Load()

    if nil == target || nil == target.writer {
        return os.Stderr.Write(data)
    }

    return target.writer.Write(data)
}

var _ io.Writer = (*retargetableDiagnosticsWriter)(nil)
