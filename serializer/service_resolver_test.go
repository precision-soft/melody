package serializer

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/runtime"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
)

/* @info the soft resolvers answer nil on failure even when the logger cannot be resolved either: the reporting branch goes through the soft logger resolver, so a runtime broken enough to lose both services costs an emergency record instead of a panic inside the very branch that reports the failure */
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

/* @info a factory handing back a typed nil is refused by the container itself with an error, and the resolver answers nil with the failure recorded — the pin covers the whole path; the resolver's own typed-nil branch stays as latent defense for a resolution path without the container's refusal */
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
