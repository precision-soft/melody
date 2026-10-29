package cors

import (
    "context"
    "errors"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
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
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.New(context.Background(), scope, serviceContainer), logger
}

func assertPanicsWithContext(t *testing.T, callback func(), expectedMessage string, key string, expectedValue string) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic with the refusal %q", expectedMessage)
        }

        var refusal *exception.Error
        if false == errors.As(recoveredErr, &refusal) {
            t.Fatalf("expected the refusal %q, got %v", expectedMessage, recoveredErr)
        }

        if expectedMessage != refusal.Message() {
            t.Fatalf("expected the message %q, got %q", expectedMessage, refusal.Message())
        }

        if expectedValue != refusal.Context()[key] {
            t.Fatalf("expected the refusal to carry %s %q, got %v", key, expectedValue, refusal.Context()[key])
        }
    }()

    callback()
}
