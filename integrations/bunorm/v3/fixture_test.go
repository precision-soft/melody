package bunorm

import (
    "errors"
    "sync"

    "github.com/uptrace/bun"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

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

func (instance *delayedDiagnosticProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    RouteDiagnostics(logger)
    close(instance.entered)
    <-instance.resume
    RouteDiagnostics(logger)

    return nil, errors.New("delayed open refused")
}

/* routingProvider routes bun's channel with the logger its open receives, as the mysql and pgsql providers do, and answers a stub database */
type routingProvider struct{}

func (instance *routingProvider) Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    RouteDiagnostics(logger)

    database, _ := newCloseRaceDatabase()

    return database, nil
}

/* errorOnlyLogger reports only the error level as enabled, the shape of a logger with a threshold */
type errorOnlyLogger struct {
    capturingDiagnosticLogger
}

func (instance *errorOnlyLogger) Enabled(level loggingcontract.Level) bool {
    return loggingcontract.LevelError == level
}
