package serializer

import (
    "context"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
)

func TestSoftResolvers_AnswerNilWhenTheLoggerIsMissingToo(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    resolvedSerializer := SerializerFromRuntime(runtimeInstance)
    if nil != resolvedSerializer {
        t.Fatalf("expected a nil serializer from an empty container, got %T", resolvedSerializer)
    }

    resolvedManager := SerializerManagerFromRuntime(runtimeInstance)
    if nil != resolvedManager {
        t.Fatalf("expected a nil serializer manager from an empty container, got %T", resolvedManager)
    }
}

func TestSerializerFromRuntime_RefusesATypedNilSerializer(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    registerErr := serviceContainer.Register(
        ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return (*JsonSerializer)(nil), nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    resolvedSerializer := SerializerFromRuntime(runtimeInstance)
    if nil != resolvedSerializer {
        t.Fatalf("expected the typed-nil serializer to be refused, got %T", resolvedSerializer)
    }
}

func TestSoftResolvers_AnswerTheRegisteredServices(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    registeredSerializer := NewJsonSerializer()
    registerSerializerErr := serviceContainer.Register(
        ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return registeredSerializer, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerSerializerErr {
        t.Fatalf("unexpected register error: %v", registerSerializerErr)
    }

    registeredManager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: registeredSerializer,
    }, SerializerManagerConfig{})
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    registerManagerErr := serviceContainer.Register(
        ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*SerializerManager, error) {
            return registeredManager, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerManagerErr {
        t.Fatalf("unexpected register error: %v", registerManagerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    if registeredSerializer != SerializerFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer to be handed back")
    }

    if registeredManager != SerializerManagerFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer manager to be handed back")
    }
}

func TestSoftResolvers_RecordTheResolutionFailureThroughTheLogger(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    recordingLogger := &recordingSerializerLogger{}
    registerErr := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return recordingLogger, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    if nil != SerializerFromRuntime(runtimeInstance) {
        t.Fatalf("expected a nil serializer from a container that has none")
    }

    if nil != SerializerManagerFromRuntime(runtimeInstance) {
        t.Fatalf("expected a nil serializer manager from a container that has none")
    }

    if 2 != len(recordingLogger.errorMessages) {
        t.Fatalf("expected both failures to be recorded, got %d: %v", len(recordingLogger.errorMessages), recordingLogger.errorMessages)
    }

    if false == strings.Contains(recordingLogger.errorMessages[0], "failed to resolve the serializer") {
        t.Fatalf("unexpected first record: %q", recordingLogger.errorMessages[0])
    }

    if false == strings.Contains(recordingLogger.errorMessages[1], "failed to resolve the serializer manager") {
        t.Fatalf("unexpected second record: %q", recordingLogger.errorMessages[1])
    }
}

func TestMustResolvers_HandBackTheRegisteredServices(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    registeredSerializer := NewJsonSerializer()
    registerSerializerErr := serviceContainer.Register(
        ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return registeredSerializer, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerSerializerErr {
        t.Fatalf("unexpected register error: %v", registerSerializerErr)
    }

    registeredManager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: registeredSerializer,
    }, SerializerManagerConfig{})
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    registerManagerErr := serviceContainer.Register(
        ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*SerializerManager, error) {
            return registeredManager, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerManagerErr {
        t.Fatalf("unexpected register error: %v", registerManagerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    if registeredSerializer != SerializerMustFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer to be handed back")
    }

    if registeredManager != SerializerManagerMustFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer manager to be handed back")
    }
}

/* both doors panic with the same message, so the service name in the context is what is asserted */
func TestSerializerMustFromRuntime_PanicsNamingTheServiceItAskedFor(t *testing.T) {
    assertMustResolverPanicsForService(t, ServiceSerializer, func(runtimeInstance containercontract.Runtime) {
        _ = SerializerMustFromRuntime(runtimeInstance)
    })
}

func TestSerializerManagerMustFromRuntime_PanicsNamingTheServiceItAskedFor(t *testing.T) {
    assertMustResolverPanicsForService(t, ServiceSerializerManager, func(runtimeInstance containercontract.Runtime) {
        _ = SerializerManagerMustFromRuntime(runtimeInstance)
    })
}

func assertMustResolverPanicsForService(t *testing.T, expectedServiceName string, callback func(runtimeInstance containercontract.Runtime)) {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    defer func() {
        t.Helper()

        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the missing service %q to panic", expectedServiceName)
        }

        recoveredErr, isContextual := recoveredValue.(interface {
            error
            Context() exceptioncontract.Context
        })
        if false == isContextual {
            t.Fatalf("expected a contextual error panic value, got %#v", recoveredValue)
        }

        if false == strings.Contains(recoveredErr.Error(), "service is not registered") {
            t.Fatalf("unexpected panic message: %q", recoveredErr.Error())
        }

        if expectedServiceName != recoveredErr.Context()["serviceName"] {
            t.Fatalf(
                "expected the panic to name %q, got %v",
                expectedServiceName,
                recoveredErr.Context()["serviceName"],
            )
        }
    }()

    callback(runtimeInstance)
}

type recordingSerializerLogger struct {
    errorMessages []string
}

func (instance *recordingSerializerLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *recordingSerializerLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *recordingSerializerLogger) Info(message string, context loggingcontract.Context) {}

func (instance *recordingSerializerLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *recordingSerializerLogger) Error(message string, context loggingcontract.Context) {
    instance.errorMessages = append(instance.errorMessages, message)
}

func (instance *recordingSerializerLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *recordingSerializerLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordingSerializerLogger) CauseChainDepth() int {
    return 8
}

var _ loggingcontract.Logger = (*recordingSerializerLogger)(nil)
