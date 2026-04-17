package output

import (
    "sync/atomic"
)

var applicationVersion atomic.Value

func SetApplicationVersion(versionString string) {
    applicationVersion.Store(versionString)
}

func getApplicationVersion() string {
    if storedValue, ok := applicationVersion.Load().(string); true == ok {
        return storedValue
    }

    return ""
}
