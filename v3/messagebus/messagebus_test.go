package messagebus

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

type taskCreated struct {
    TaskId int
}

func newTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

/* messageRecordingLogger captures every record with its message, so a test can assert both that a record was written and what it said. */
type messageRecordingLogger struct {
    mutex    sync.Mutex
    messages []string
}

func (instance *messageRecordingLogger) record(message string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.messages = append(instance.messages, message)
}

func (instance *messageRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.record(message)
}

func (instance *messageRecordingLogger) Debug(message string, context loggingcontract.Context) {
    instance.record(message)
}

func (instance *messageRecordingLogger) Info(message string, context loggingcontract.Context) {
    instance.record(message)
}

func (instance *messageRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.record(message)
}

func (instance *messageRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.record(message)
}

func (instance *messageRecordingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.record(message)
}

func (instance *messageRecordingLogger) hasMessageContaining(fragment string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, message := range instance.messages {
        if true == strings.Contains(message, fragment) {
            return true
        }
    }

    return false
}

/* newTestRuntimeWithRecordingLogger mirrors newTestRuntime with the recording logger installed under the logger service, the way a framework-assembled scope carries one. */
func newTestRuntimeWithRecordingLogger() (runtimecontract.Runtime, *messageRecordingLogger) {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    logger := &messageRecordingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)

    return runtime.New(context.Background(), scope, serviceContainer), logger
}
