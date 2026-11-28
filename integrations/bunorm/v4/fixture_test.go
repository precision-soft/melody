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

func testRuntime() containercontract.Runtime {
    return testRuntimeUnder(context.Background())
}

func testRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return melodyruntime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

var testConnectionParameters = ConnectionParameters{Host: "127.0.0.1", Database: "melody"}

func expectErrorNamingField(t *testing.T, err error, field string) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected a refusal naming %s, got none", field)
    }

    if field != exception.LogContext(err, exception.LogContextOptions{})["field"] {
        t.Fatalf("expected the refusal to name %s, got %v: %v", field, exception.LogContext(err, exception.LogContextOptions{}), err)
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

func (instance *capturingDiagnosticLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *capturingDiagnosticLogger) CauseChainDepth() int {
    return 8
}

func (instance *capturingDiagnosticLogger) captured() []capturedDiagnosticRecord {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]capturedDiagnosticRecord{}, instance.records...)
}

type delayedDiagnosticProvider struct {
    entered chan struct{}
    resume  chan struct{}
}

func (instance *delayedDiagnosticProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    RouteDiagnostics(logger)
    close(instance.entered)
    <-instance.resume
    RouteDiagnostics(logger)

    return nil, errors.New("delayed open refused")
}

func (instance *delayedDiagnosticProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

type routingProvider struct{}

func (instance *routingProvider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    RouteDiagnostics(logger)

    database, _ := newCloseRaceDatabase()

    return database, nil
}

func (instance *routingProvider) OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.Open(runtimeInstance, parameters, logger)
}

type errorOnlyLogger struct {
    capturingDiagnosticLogger
}

func (instance *errorOnlyLogger) Enabled(level loggingcontract.Level) bool {
    return loggingcontract.LevelError == level
}
