package output

import (
    "sync/atomic"
)

var applicationVersion atomic.Value

/* SetApplicationVersion declares the running application's own version to every command envelope, read by the meta of each document and by debug:version. Call it once from the composition root before Run. Undeclared, it renders empty in the meta and <unknown> in debug:version. */
func SetApplicationVersion(versionString string) {
    applicationVersion.Store(versionString)
}

func getApplicationVersion() string {
    if storedValue, ok := applicationVersion.Load().(string); true == ok {
        return storedValue
    }

    return ""
}
