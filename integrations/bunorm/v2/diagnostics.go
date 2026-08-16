package bunorm

import (
    "sync"

    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/uptrace/bun"
)

var bunDiagnosticsOnce sync.Once

/* RouteDiagnostics sends bun's own diagnostic channel to the application's journal. Bun reports the developer's declaration mistakes through a package-level logger of its own — an unknown struct tag option, an unknown on_update or on_delete rule on a relation, a query carrying arguments and no placeholders — and unrouted they are written to standard error as unstructured text, invisible to a deployment whose journal is a json file. They arrive as warning records carrying the line, the shape NewStandardErrorLogger already gives net/http's own reporting.

Bun's logger is one variable for the whole process, so the first provider to open wins it and later calls are ignored: an application with two managers has one journal anyway. The once outlives every registry: in a process that builds, closes and rebuilds its application — a test binary above all — the first lifecycle's logger keeps the channel for the process's whole life, and if that logger is closed (or was the emergency fallback of a resolver without a logging service) the later lifecycles' bun diagnostics are dropped silently. It is called from the providers rather than from a module hook because a binary that never opens a database has no reason to take the setting at all.

It does not reach the one line the mysql dialect writes when it cannot read the server version. That line goes through the standard library's own default logger, not through bun's, so routing it means taking log.SetOutput for the whole process — every dependency and the application's own log calls with it — which is the application's decision to make and not this package's. See the mysql readme. */
func RouteDiagnostics(logger loggingcontract.Logger) {
    if nil == logger {
        return
    }

    bunDiagnosticsOnce.Do(func() {
        installBunDiagnostics(logger)
    })
}

/* installBunDiagnostics performs the setting itself, apart from the once that guards it, so the destination can be proven without the guard standing in the way of a second proof. */
func installBunDiagnostics(logger loggingcontract.Logger) {
    bun.SetLogger(logging.NewStandardErrorLogger(logger, "bun diagnostic"))
}
