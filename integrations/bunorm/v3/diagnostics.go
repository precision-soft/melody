package bunorm

import (
    "io"
    "log"
    "os"
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

/* diagnosticsTarget holds the writer one routing installed. The writer is built once per routing rather than per record: it is logging.NewStandardErrorLogger's own, so the record shape stays the framework's and is not spelled a second time here. */
type diagnosticsTarget struct {
    writer io.Writer
}

/* RouteDiagnostics sends bun's own diagnostic channel to the application's journal. Bun reports the developer's declaration mistakes through a package-level logger of its own — an unknown struct tag option, an unknown on_update or on_delete rule on a relation, a query carrying arguments and no placeholders — and unrouted they are written to standard error as unstructured text, invisible to a deployment whose journal is a json file. They arrive as warning records carrying the line, the shape NewStandardErrorLogger already gives net/http's own reporting.

   Bun's logger is one variable for the whole process, so it is set exactly once — but what is set is a forwarder onto a destination this function REPLACES on every call, and the destination is what decides where a record goes. The distinction is the whole point: a process that builds, closes and rebuilds its application — a test binary above all, but equally an application wired before its own logger exists — used to leave bun's channel pinned to the first lifecycle's logger for the life of the process, so every later lifecycle's diagnostics were dropped into a logger that was closed, or into an emergency fallback nobody reads. Now the first routing of each lifecycle takes the channel back.

   A nil logger, and a typed nil holding no value, route nothing: they are the wiring mistake this package refuses everywhere else, and installing one as the destination would drop records against a receiver that cannot take them.

   It does not reach the one line the mysql dialect writes when it cannot read the server version. That line goes through the standard library's own default logger, not through bun's, so routing it means taking log.SetOutput for the whole process — every dependency and the application's own log calls with it — which is the application's decision to make and not this package's. See the mysql readme. */
func RouteDiagnostics(logger loggingcontract.Logger) {
    if nil == logger || true == isNilInterface(logger) {
        return
    }

    bunDiagnosticsTarget.Store(newDiagnosticsTarget(logger))

    bunDiagnosticsOnce.Do(installBunDiagnostics)
}

/* newDiagnosticsTarget builds the destination one routing installs. The writer is built once per routing rather than per record, and it is logging.NewStandardErrorLogger's own, so the record shape stays the framework's and is not spelled a second time here. */
func newDiagnosticsTarget(logger loggingcontract.Logger) *diagnosticsTarget {
    return &diagnosticsTarget{
        writer: logging.NewStandardErrorLogger(logger, bunDiagnosticsMessage).Writer(),
    }
}

/* ResetDiagnostics hands bun's diagnostic channel back: the records go to standard error again, which is where bun writes them when no one routes them at all. It is what a teardown calls while the logger it routed to is still alive, so nothing is written into a journal that is closing — the ManagerRegistry calls it from its own Close for exactly that reason, and the container's teardown order puts the registry ahead of the logging service because the registry resolves it.

   It is also the door for a process that hosts melody rather than being one: a binary with its own reporting for bun takes the channel back with this and sets its own logger afterwards. Calling it when nothing was ever routed is not an error and changes nothing. */
func ResetDiagnostics() {
    bunDiagnosticsTarget.Store(nil)
}

/* installBunDiagnostics performs the setting itself, apart from the once that guards it, so the destination can be proven without the guard standing in the way of a second proof. */
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
