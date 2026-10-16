package cors

import (
    "context"
    "sync"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* warningRecordingLogger keeps the context of every warning, so a test can read the boot warnings a service wrote by their name */
type warningRecordingLogger struct {
    mutex    sync.Mutex
    contexts []loggingcontract.Context
}

func (instance *warningRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.contexts = append(instance.contexts, context)
}

func (instance *warningRecordingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *warningRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) entriesOfBootWarning(name string) []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    entries := make([]string, 0)
    for _, context := range instance.contexts {
        if name == context["bootWarning"] {
            entries = append(entries, context["entry"].(string))
        }
    }

    return entries
}

func runtimeWithWarningRecordingLogger() (runtimecontract.Runtime, *warningRecordingLogger) {
    logger := &warningRecordingLogger{}

    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)

    return runtime.New(context.Background(), scope, serviceContainer), logger
}
