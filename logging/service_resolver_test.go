package logging

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
)

/* @info a factory handing back a typed nil is refused by the container itself with an error, and the resolver answers nil with the failure recorded — the pin covers the whole path; the resolver's own typed-nil branch stays as latent defense for a resolution path without the container's refusal */
func TestLoggerFromRuntime_RefusesATypedNilLogger(t *testing.T) {
    serviceContainer := container.NewContainer()

    registerErr := serviceContainer.Register(
        ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return (*jsonLogger)(nil), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    resolvedLogger := LoggerFromRuntime(runtimeInstance)

    if nil != resolvedLogger {
        t.Fatalf("expected the typed-nil logger to be refused, got %T", resolvedLogger)
    }
}

/* @info the two resolver-shaped doors are what a scoped service actually calls — the container and runtime doors above them are the boot-time pair — and neither had ever been entered. Both must find the logger under the one service name the whole framework agrees on */
func TestLoggerFromResolverDoors_FindTheRegisteredLogger(t *testing.T) {
    serviceContainer := container.NewContainer()

    registeredLogger := NewNopLogger()

    registerErr := serviceContainer.Register(
        ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return registeredLogger, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    resolvedLogger, resolveErr := LoggerFromResolver(serviceContainer)
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if registeredLogger != resolvedLogger {
        t.Fatalf("expected the registered logger, got %T", resolvedLogger)
    }

    if registeredLogger != LoggerMustFromResolver(serviceContainer) {
        t.Fatalf("expected the must-door to return the same logger")
    }
}

/* @info the soft door reports the failure it cannot serve instead of answering an error the caller drops: a container with no logger registered is a boot mistake, and the error is the only thing that says so */
func TestLoggerFromResolver_WithoutARegisteredLogger_ReportsTheFailure(t *testing.T) {
    serviceContainer := container.NewContainer()

    resolvedLogger, resolveErr := LoggerFromResolver(serviceContainer)

    if nil == resolveErr {
        t.Fatalf("expected an error for a container with no logger, got %T", resolvedLogger)
    }

    if nil != resolvedLogger {
        t.Fatalf("expected no logger beside the error, got %T", resolvedLogger)
    }
}

/* @info the must-door is the one a resolution failure must not pass silently: it panics where the caller's assumption breaks instead of handing back a nil that dereferences one frame later */
func TestLoggerMustFromResolver_WithoutARegisteredLogger_Panics(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected a panic for a container with no logger")
        }
    }()

    _ = LoggerMustFromResolver(serviceContainer)
}

/* @info the soft runtime door answers nil for a resolution that failed with an error, and the failure leaves an emergency record: the caller's own nil check then skips its own record, so without this one the failure would vanish twice */
func TestLoggerFromRuntime_ResolutionError_AnswersNil(t *testing.T) {
    serviceContainer := container.NewContainer()

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    if nil != LoggerFromRuntime(runtimeInstance) {
        t.Fatalf("expected nil when the logger cannot be resolved")
    }
}

/* @info the runtime doors read the same service name as the resolver doors; a divergence between the two would leave half the framework logging into a logger nothing else can find */
func TestLoggerFromRuntime_FindsTheRegisteredLogger(t *testing.T) {
    serviceContainer := container.NewContainer()

    registeredLogger := NewNopLogger()

    registerErr := serviceContainer.Register(
        ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return registeredLogger, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    if registeredLogger != LoggerFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered logger from the soft runtime door")
    }

    if registeredLogger != LoggerMustFromRuntime(runtimeInstance) {
        t.Fatalf("expected the registered logger from the must runtime door")
    }

    resolvedLogger, resolveErr := LoggerFromContainer(serviceContainer)
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if registeredLogger != resolvedLogger {
        t.Fatalf("expected the registered logger from the container door")
    }

    if registeredLogger != LoggerMustFromContainer(serviceContainer) {
        t.Fatalf("expected the registered logger from the must container door")
    }
}
