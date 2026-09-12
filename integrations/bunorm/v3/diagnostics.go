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

/* diagnosticsTarget holds the writer one routing installed, and the logger it was built for. The writer is built once per LOGGER rather than per record or per routing: it is logging.NewStandardErrorLogger's own, so the record shape stays the framework's and is not spelled a second time here; and the logger is kept so a repeated routing on the same logger installs nothing and a teardown hands the channel back only when the channel is its own. */
type diagnosticsTarget struct {
    logger loggingcontract.Logger
    writer io.Writer
}

/* RouteDiagnostics sends bun's own diagnostic channel to the application's journal. Bun reports the developer's declaration mistakes through a package-level logger of its own — an unknown struct tag option, an unknown on_update or on_delete rule on a relation, a query carrying arguments and no placeholders — and unrouted they are written to standard error as unstructured text, invisible to a deployment whose journal is a json file. They arrive as warning records carrying the line, the shape NewStandardErrorLogger already gives net/http's own reporting.

   Bun's logger is one variable for the whole process, so it is set exactly once — but what is set is a forwarder onto a destination this function replaces whenever it is called with a DIFFERENT logger, and the destination is what decides where a record goes. The distinction is the whole point: a process that builds, closes and rebuilds its application — a test binary above all, but equally an application wired before its own logger exists — used to leave bun's channel pinned to the first lifecycle's logger for the life of the process, so every later lifecycle's diagnostics were dropped into a logger that was closed, or into an emergency fallback nobody reads. Now the first routing of each lifecycle takes the channel back. A routing on the logger already installed changes nothing and allocates nothing: the providers route on every open, and a destination rebuilt per open was a writer allocated per open for the same journal.

   A nil logger, and a typed nil holding no value, route nothing: they are the wiring mistake this package refuses everywhere else, and installing one as the destination would drop records against a receiver that cannot take them.

   It does not reach the one line the mysql dialect writes when it cannot read the server version. That line goes through the standard library's own default logger, not through bun's, so routing it means taking log.SetOutput for the whole process — every dependency and the application's own log calls with it — which is the application's decision to make and not this package's. See the mysql readme. */
func RouteDiagnostics(logger loggingcontract.Logger) {
    routeDiagnosticsTo(logger)
}

/* routeDiagnosticsTo is RouteDiagnostics answering the destination it left live — the one already installed for this logger, or the one it installed — so a registry can keep the destination it routed and hand exactly that one back at its Close, whether or not its logger can be compared. A logger whose dynamic type carries no identity — held by value with a slice, a map or a func inside — is routed afresh on every call, a writer per open for the same journal, which is the cost of not reading its content; nil, and a typed nil, route nothing and answer nil. */
func routeDiagnosticsTo(logger loggingcontract.Logger) *diagnosticsTarget {
    if nil == logger || true == isNilInterface(logger) {
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

/* resetDiagnosticsRoutedTo hands bun's diagnostic channel back only when the live destination is the one routed to this logger, or the very destination the caller routed. It is what a registry's Close calls: the process may hold two registries — two applications in one test binary, or a second registry wired beside the first — and a Close that reset the channel unconditionally took it away from the registry still running, whose diagnostics went to standard error until its next open routed them again. Two registries sharing one logger still share one channel, and the first to close hands it back for both; the next open of the other takes it again. The destination the caller routed is asked of by identity because a logger held by value with a slice, a map or a func inside has none: read by content instead, two distinct doubles of equal content were one logger, so the Close of one registry took the channel of another, and the read itself walked the logger's fields under no lock, a data race with the logger's own Log — measured, both. What such a logger costs is written on routeDiagnosticsTo: a destination a provider routed afresh for a copy of it is not the one the registry kept, and stays routed after its Close. */
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

/* isSameLogger answers whether two loggers are one value, without the panic a bare comparison of two interfaces carries: Logger is the most implemented contract melody has — a test double, an integrator's adapter — and a value whose dynamic type holds a slice, a map or a func is not comparable, so `logger == live.logger` was a runtime panic on the SECOND routing, or at the registry's Close through the hand-back, for a logger that had routed fine once. Such a value has no identity to answer for, and none is invented for it: two of them are never the same, so a routing on one is never deduplicated and a hand-back on one goes through the destination the caller kept. */
func isSameLogger(left loggingcontract.Logger, right loggingcontract.Logger) bool {
    if nil == left || nil == right {
        return nil == left && nil == right
    }

    if false == reflect.ValueOf(left).Comparable() || false == reflect.ValueOf(right).Comparable() {
        return false
    }

    return left == right
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
