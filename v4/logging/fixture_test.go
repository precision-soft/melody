package logging

import (
    "os"
    "syscall"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* the capturing double stands in for whatever logger a suite exercises; the logger, recover, json-logger and request-logger tests all reach it from here. */
type captureLogger struct {
    lastLevel   loggingcontract.Level
    lastMessage string
    lastContext map[string]any
    calls       int
}

func (instance *captureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.calls++
    instance.lastLevel = level
    instance.lastMessage = message
    instance.lastContext = context
}

func (instance *captureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *captureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *captureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *captureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *captureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *captureLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *captureLogger) CauseChainDepth() int {
    return 8
}

var _ loggingcontract.Logger = (*captureLogger)(nil)

/* typedNilProbeError is the concrete error type whose typed nil the package has to read as the nil its producer meant, rather than dereference. */
type typedNilProbeError struct {
    message string
}

func (instance *typedNilProbeError) Error() string {
    return instance.message
}

/* recoveredPanicContext answers the context of the *exception.Error the callback panics with, nil when it does not. */
func recoveredPanicContext(callback func()) (context exceptioncontract.Context) {
    defer func() {
        if exceptionErr, isExceptionError := recover().(*exception.Error); true == isExceptionError && nil != exceptionErr {
            context = exceptionErr.Context()
        }
    }()

    callback()

    return nil
}

/* the umask is process-wide: no logging test runs in parallel, so setting it here narrows no other test's files */
func withZeroUmask(t *testing.T) {
    t.Helper()

    previousUmask := syscall.Umask(0)
    t.Cleanup(func() { syscall.Umask(previousUmask) })
}

func assertPermission(t *testing.T, path string, expected os.FileMode) {
    t.Helper()

    information, statErr := os.Stat(path)
    if nil != statErr {
        t.Fatalf("unexpected stat error on %s: %v", path, statErr)
    }

    if expected != information.Mode().Perm() {
        t.Fatalf("expected %s at mode %o, got %o", path, expected, information.Mode().Perm())
    }
}
