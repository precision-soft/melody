package messagebus

import (
    "context"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
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
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.New(context.Background(), scope, serviceContainer), logger
}

func mustConsumeSession(command *ConsumeCommand, runtimeInstance runtimecontract.Runtime) *consumeSession {
    session, sessionErr := command.newConsumeSession(runtimeInstance)
    if nil != sessionErr {
        panic(sessionErr)
    }

    return session
}

func registerConsumeServices(t *testing.T, serviceContainer containercontract.Container, policy RetryPolicy) {
    t.Helper()

    serviceContainer.MustRegister(ServiceConsumeBus, func(containercontract.Resolver) (messagebuscontract.Bus, error) {
        return NewManager("consume", nil), nil
    }, container.RegisterOptions{})
    serviceContainer.MustRegister(ServiceRetryPolicy, func(containercontract.Resolver) (RetryPolicy, error) {
        return policy, nil
    }, container.RegisterOptions{})
    RegisterTransports(
        transportRegistrarAdapter{serviceContainer: serviceContainer},
        map[string]messagebuscontract.Transport{"async": NewInMemoryTransport(InMemoryTransportConfig{})},
    )
}

func recoveredPanicContext(callback func()) (context exceptioncontract.Context) {
    defer func() {
        if exceptionErr, isExceptionError := recover().(*exception.Error); true == isExceptionError && nil != exceptionErr {
            context = exceptionErr.Context()
        }
    }()

    callback()

    return nil
}
