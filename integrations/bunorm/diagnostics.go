package bunorm

import (
    "sync"

    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/uptrace/bun"
)

var bunDiagnosticsOnce sync.Once

/* RouteDiagnostics sends bun's own diagnostic channel, which reports declaration mistakes such as an unknown struct tag option, an unknown on_update or on_delete rule or a query with arguments and no placeholders, to the application's journal as warning records instead of standard error. Bun's logger is one variable for the process, so the first provider to open wins it for the process's whole life and later calls are ignored: in a process that rebuilds its application, a test binary above all, a later lifecycle's diagnostics go to the first lifecycle's logger and are dropped if that logger is closed. It is called from the providers, so a binary that never opens a database takes no setting. The mysql dialect's server-version line goes through the standard library's default logger and is not reached; see the mysql readme. */
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
