package mailer

import (
    "context"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

func testRuntime() containercontract.Runtime {
    return testRuntimeWithContext(context.Background())
}

func testRuntimeWithContext(ctx context.Context) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRun(ctx, serviceContainer.NewScope())
}

type nilRuntime struct{}

func (instance *nilRuntime) Context() context.Context {
    return context.Background()
}

func (instance *nilRuntime) Scope() containercontract.Scope {
    return nil
}

func (instance *nilRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

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

func (instance *mailerRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *mailerRecordingLogger) CauseChainDepth() int {
    return 8
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

func testRuntimeWithRecordingLogger() (containercontract.Runtime, *mailerRecordingLogger) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    logger := &mailerRecordingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.NewRun(context.Background(), scope), logger
}

func sendErrorOf(receipt mailercontract.Receipt, sendErr error) error {
    return sendErr
}
