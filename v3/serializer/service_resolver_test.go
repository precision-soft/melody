package serializer

import (
    "context"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

func TestSoftResolvers_AnswerNilWhenTheLoggerIsMissingToo(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    resolvedSerializer := SerializerFromRuntime(runtimeInstance)
    if nil != resolvedSerializer {
        t.Fatalf("expected a nil serializer from an empty container, got %T", resolvedSerializer)
    }

    resolvedManager := SerializerManagerFromRuntime(runtimeInstance)
    if nil != resolvedManager {
        t.Fatalf("expected a nil serializer manager from an empty container, got %T", resolvedManager)
    }
}

/* the container itself refuses a factory handing back a typed nil, so the resolver's own typed-nil branch is LATENT: this pins the whole path, not that branch. */
func TestSerializerFromRuntime_RefusesATypedNilSerializer(t *testing.T) {
    serviceContainer := container.NewContainer()

    registerErr := serviceContainer.Register(
        ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return (*JsonSerializer)(nil), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    resolvedSerializer := SerializerFromRuntime(runtimeInstance)
    if nil != resolvedSerializer {
        t.Fatalf("expected the typed-nil serializer to be refused, got %T", resolvedSerializer)
    }
}

func TestSoftResolvers_AnswerTheRegisteredServices(t *testing.T) {
    serviceContainer := container.NewContainer()

    registeredSerializer := NewJsonSerializer()
    registerSerializerErr := serviceContainer.Register(
        ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return registeredSerializer, nil
        },
    )
    if nil != registerSerializerErr {
        t.Fatalf("unexpected register error: %v", registerSerializerErr)
    }

    registeredManager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: registeredSerializer,
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    registerManagerErr := serviceContainer.Register(
        ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*SerializerManager, error) {
            return registeredManager, nil
        },
    )
    if nil != registerManagerErr {
        t.Fatalf("unexpected register error: %v", registerManagerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    if registeredSerializer != SerializerFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer to be handed back")
    }

    if registeredManager != SerializerManagerFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer manager to be handed back")
    }
}

/* the empty-container pin above proves only that the reporting branch does not panic: a resolver that answered nil without recording anything looks identical from the caller's side, which is why the record is asserted here against a resolvable logger. */
func TestSoftResolvers_RecordTheResolutionFailureThroughTheLogger(t *testing.T) {
    serviceContainer := container.NewContainer()

    recordingLogger := &recordingSerializerLogger{}
    registerErr := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return recordingLogger, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

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
    serviceContainer := container.NewContainer()

    registeredSerializer := NewJsonSerializer()
    registerSerializerErr := serviceContainer.Register(
        ServiceSerializer,
        func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
            return registeredSerializer, nil
        },
    )
    if nil != registerSerializerErr {
        t.Fatalf("unexpected register error: %v", registerSerializerErr)
    }

    registeredManager, managerErr := NewSerializerManager(map[string]serializercontract.Serializer{
        MimeApplicationJson: registeredSerializer,
    })
    if nil != managerErr {
        t.Fatalf("unexpected manager error: %v", managerErr)
    }

    registerManagerErr := serviceContainer.Register(
        ServiceSerializerManager,
        func(resolver containercontract.Resolver) (*SerializerManager, error) {
            return registeredManager, nil
        },
    )
    if nil != registerManagerErr {
        t.Fatalf("unexpected register error: %v", registerManagerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    if registeredSerializer != SerializerMustFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer to be handed back")
    }

    if registeredManager != SerializerManagerMustFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered serializer manager to be handed back")
    }
}

/* both doors panic with the same message, so a message-only assertion is SHADOWED: the service name asked for travels in the context, and that is what each test reads. */
func TestSerializerMustFromRuntime_PanicsNamingTheServiceItAskedFor(t *testing.T) {
    assertMustResolverPanicsForService(t, ServiceSerializer, func(runtimeInstance runtimecontract.Runtime) {
        _ = SerializerMustFromRuntime(runtimeInstance)
    })
}

func TestSerializerManagerMustFromRuntime_PanicsNamingTheServiceItAskedFor(t *testing.T) {
    assertMustResolverPanicsForService(t, ServiceSerializerManager, func(runtimeInstance runtimecontract.Runtime) {
        _ = SerializerManagerMustFromRuntime(runtimeInstance)
    })
}

func assertMustResolverPanicsForService(t *testing.T, expectedServiceName string, callback func(runtimeInstance runtimecontract.Runtime)) {
    t.Helper()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

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

var _ loggingcontract.Logger = (*recordingSerializerLogger)(nil)
