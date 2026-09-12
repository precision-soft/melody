package logging

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
)

/* the container itself refuses a factory handing back a typed nil, so the resolver's own typed-nil branch is LATENT: this pins the whole path, not that branch. */
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

func TestLoggerMustFromResolver_WithoutARegisteredLogger_Panics(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected a panic for a container with no logger")
        }
    }()

    _ = LoggerMustFromResolver(serviceContainer)
}

func TestLoggerFromRuntime_ResolutionError_AnswersNil(t *testing.T) {
    serviceContainer := container.NewContainer()

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    if nil != LoggerFromRuntime(runtimeInstance) {
        t.Fatalf("expected nil when the logger cannot be resolved")
    }
}

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
