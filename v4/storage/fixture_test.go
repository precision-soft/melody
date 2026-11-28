package storage

import (
    "syscall"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

func recoveredPanicContext(callback func()) (context exceptioncontract.Context) {
    defer func() {
        if exceptionErr, isExceptionError := recover().(*exception.Error); true == isExceptionError && nil != exceptionErr {
            context = exceptionErr.Context()
        }
    }()

    callback()

    return nil
}

/* the umask is process-wide: no storage test runs in parallel, so a test asserting a created mode sets it to zero and restores it */
func withZeroUmask(t *testing.T) {
    t.Helper()

    previousUmask := syscall.Umask(0)
    t.Cleanup(func() { syscall.Umask(previousUmask) })
}
