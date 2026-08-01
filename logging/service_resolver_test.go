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
