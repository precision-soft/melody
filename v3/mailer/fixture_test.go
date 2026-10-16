package mailer

import (
    "context"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func testRuntime() runtimecontract.Runtime {
    return testRuntimeWithContext(context.Background())
}

func testRuntimeWithContext(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

/* nilRuntime exists so a test can hand a TYPED nil runtime through the Runtime interface: the interface value then carries a type and a nil pointer, which a plain == nil comparison waves through. */
type nilRuntime struct{}

func (instance *nilRuntime) Context() context.Context {
    return context.Background()
}

func (instance *nilRuntime) Scope() containercontract.Scope {
    return nil
}

func (instance *nilRuntime) Container() containercontract.Container {
    return nil
}

/* mailerRecordingLogger captures every record's message, so a test can assert what a warning said. */
type mailerRecordingLogger struct {
    mutex    sync.Mutex
    messages []string
    contexts []loggingcontract.Context
}

func (instance *mailerRecordingLogger) record(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.messages = append(instance.messages, message)
    instance.contexts = append(instance.contexts, context)
}

func (instance *mailerRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.record(message, context)
}

func (instance *mailerRecordingLogger) Debug(message string, context loggingcontract.Context) {
    instance.record(message, context)
}

func (instance *mailerRecordingLogger) Info(message string, context loggingcontract.Context) {
    instance.record(message, context)
}

func (instance *mailerRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.record(message, context)
}

func (instance *mailerRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.record(message, context)
}

func (instance *mailerRecordingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.record(message, context)
}

func (instance *mailerRecordingLogger) contextOfMessage(fragment string) (loggingcontract.Context, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for index, message := range instance.messages {
        if true == strings.Contains(message, fragment) {
            return instance.contexts[index], true
        }
    }

    return nil, false
}

/* testRuntimeWithRecordingLogger mirrors testRuntime with the recording logger installed under the logger service. */
func testRuntimeWithRecordingLogger() (runtimecontract.Runtime, *mailerRecordingLogger) {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    logger := &mailerRecordingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger)

    return runtime.New(context.Background(), scope, serviceContainer), logger
}
