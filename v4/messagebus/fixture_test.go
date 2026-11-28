package messagebus

import (
    "context"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

type taskCreated struct {
    TaskId int
}

func newTestRuntime() containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}

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

func (instance *messageRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *messageRecordingLogger) CauseChainDepth() int {
    return 8
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

func newTestRuntimeWithRecordingLogger() (containercontract.Runtime, *messageRecordingLogger) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()

    logger := &messageRecordingLogger{}
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    return runtime.NewRun(context.Background(), scope), logger
}

type consumeSessionSource interface {
    newConsumeSession(runtimeInstance containercontract.Runtime) (*consumeSession, error)
}

type consumeCommandOverBus struct {
    *ConsumeCommand
    bus        messagebuscontract.Bus
    transports map[string]messagebuscontract.Transport
}

func newConsumeCommandOver(bus messagebuscontract.Bus, config ConsumeCommandConfig) *consumeCommandOverBus {
    return &consumeCommandOverBus{ConsumeCommand: NewConsumeCommand(config), bus: bus}
}

func newConsumeCommandOverTransports(bus messagebuscontract.Bus, config ConsumeCommandConfig, transports map[string]messagebuscontract.Transport) *consumeCommandOverBus {
    return &consumeCommandOverBus{ConsumeCommand: NewConsumeCommand(config), bus: bus, transports: transports}
}

func (instance *consumeCommandOverBus) newConsumeSession(runtimeInstance containercontract.Runtime) (*consumeSession, error) {
    return instance.sessionOver(instance.bus, instance.transports)
}

func mustConsumeSession(command consumeSessionSource, runtimeInstance containercontract.Runtime) *consumeSession {
    session, sessionErr := command.newConsumeSession(runtimeInstance)
    if nil != sessionErr {
        panic(sessionErr)
    }

    return session
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

type queueSubscription struct {
    queue chan messagebuscontract.Envelope
    err   error
}

func (instance *queueSubscription) Envelopes() <-chan messagebuscontract.Envelope {
    return instance.queue
}

func (instance *queueSubscription) Err() error {
    return instance.err
}
