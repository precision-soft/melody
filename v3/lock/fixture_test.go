package lock

import (
    "context"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* recordingLogger captures every record with its level and message, so a test can assert both that a record was written and what it said. */
type recordingLogger struct {
    mutex   sync.Mutex
    records []recordedLogEntry
}

type recordedLogEntry struct {
    level   string
    message string
    context loggingcontract.Context
}

func (instance *recordingLogger) record(level string, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, recordedLogEntry{level: level, message: message, context: context})
}

func (instance *recordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.record("log", message, context)
}

func (instance *recordingLogger) Debug(message string, context loggingcontract.Context) {
    instance.record("debug", message, context)
}

func (instance *recordingLogger) Info(message string, context loggingcontract.Context) {
    instance.record("info", message, context)
}

func (instance *recordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.record("warning", message, context)
}

func (instance *recordingLogger) Error(message string, context loggingcontract.Context) {
    instance.record("error", message, context)
}

func (instance *recordingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.record("emergency", message, context)
}

func (instance *recordingLogger) hasMessageContaining(fragment string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, entry := range instance.records {
        if true == strings.Contains(entry.message, fragment) {
            return true
        }
    }

    return false
}

/* runtimeWithRecordingLogger builds a runtime whose scope carries the recording logger under the logger service, mirroring how a framework-assembled scope resolves logging.LoggerFromRuntime. */
func runtimeWithRecordingLogger(ctx context.Context) (runtimecontract.Runtime, *recordingLogger) {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    logger := &recordingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)

    return runtime.New(ctx, scope, serviceContainer), logger
}
