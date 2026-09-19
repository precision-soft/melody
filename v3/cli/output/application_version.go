package output

import (
    "sync/atomic"
)

var applicationVersion atomic.Value

/* SetApplicationVersion declares the running application's own version to every command envelope: the meta of each rendered document and the application row of debug:version read it. An application calls this once from its composition root before Run, with the version it keeps wherever it keeps it — its own ldflags variable, its environment, a file. Undeclared, the application version renders empty in the meta and as <unknown> in debug:version; nothing substitutes melody's version for it. */
func SetApplicationVersion(versionString string) {
    applicationVersion.Store(versionString)
}

func getApplicationVersion() string {
    if storedValue, ok := applicationVersion.Load().(string); true == ok {
        return storedValue
    }

    return ""
}
