package bunorm

import (
    "context"
    "errors"
    "sync"
    "testing"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* testRuntime is the runtime the registry tests call under: a run over a scope of an empty container, on a context that never ends */
func testRuntime() containercontract.Runtime {
    return testRuntimeUnder(context.Background())
}

func testRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return melodyruntime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

/* the connection every definition of these tests declares: the registry refuses an empty host and an empty database, and none of the fake providers dials it */
var testConnectionParameters = ConnectionParameters{Host: "127.0.0.1", Database: "melody"}

func expectErrorNamingField(t *testing.T, err error, field string) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected a refusal naming %s, got none", field)
    }

    if field != exception.LogContext(err, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name %s, got %v: %v", field, exception.LogContext(err, exception.LogContextConfig{}), err)
    }
}

func expectPanicNamingField(t *testing.T, field string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming %s, got no error panic", field)
        }

        expectErrorNamingField(t, recoveredErr, field)
    }()

    callback()
}

/* capturingDiagnosticLogger is the journal double the routing probes read, shared by the tests of the diagnostics sink and of the registry door that replaces it. Every method DEREFERENCES the receiver, which is what lets a typed-nil guard die: a double tolerating a nil receiver would pass with the guard removed.

   It lives here rather than beside either of them because it is the shared material of two mirrors, and the layout rule keeps that in one fixture. */
type capturingDiagnosticLogger struct {
    mutex   sync.Mutex
    records []capturedDiagnosticRecord
}

type capturedDiagnosticRecord struct {
    level   loggingcontract.Level
    message string
    context loggingcontract.Context
}

func (instance *capturingDiagnosticLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, capturedDiagnosticRecord{level: level, message: message, context: context})
}

func (instance *capturingDiagnosticLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *capturingDiagnosticLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *capturingDiagnosticLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *capturingDiagnosticLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *capturingDiagnosticLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *capturingDiagnosticLogger) captured() []capturedDiagnosticRecord {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]capturedDiagnosticRecord{}, instance.records...)
}

/* delayedDiagnosticProvider routes bun's channel as a provider does at open, then waits for resume and routes again, the open still in flight when its registry closes or replaces its logger */
type delayedDiagnosticProvider struct {
    entered chan struct{}
    resume  chan struct{}
}

func (instance *delayedDiagnosticProvider) Open(runtimeInstance containercontract.Runtime, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    RouteDiagnostics(logger)
    close(instance.entered)
    <-instance.resume
    RouteDiagnostics(logger)

    return nil, errors.New("delayed open refused")
}

func (instance *delayedDiagnosticProvider) OpenForMigration(runtimeInstance containercontract.Runtime, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, params, logger)
}

/* routingProvider routes bun's channel with the logger its open receives, as the mysql and pgsql providers do, and answers a stub database */
type routingProvider struct{}

func (instance *routingProvider) Open(runtimeInstance containercontract.Runtime, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    RouteDiagnostics(logger)

    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *routingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, params, logger)
}

/* errorOnlyLogger reports only the error level as enabled, the shape of a logger with a threshold */
type errorOnlyLogger struct {
    capturingDiagnosticLogger
}

func (instance *errorOnlyLogger) Enabled(level loggingcontract.Level) bool {
    return loggingcontract.LevelError == level
}
