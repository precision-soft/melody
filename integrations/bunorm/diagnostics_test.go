package bunorm

import (
    "fmt"
    "strings"
    "sync"
    "testing"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/uptrace/bun/schema"
)

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

/* bun's own diagnostics reach the application's journal instead of standard error. They are the developer's declaration mistakes — here a query carrying an argument with nowhere to put it — and written to standard error as unstructured text they are invisible to a deployment whose journal is a json file.

The warning is provoked through bun's public surface rather than by calling its logger, because what has to be proven is that bun uses the destination this package installs, not that a *log.Logger writes where it was pointed. The setting is taken through installBunDiagnostics rather than through the exported door, whose once — correct for a process-wide setting — would make every run after the first prove nothing. */
func TestRouteDiagnostics_SendsBunsOwnChannelToTheJournal(t *testing.T) {
    logger := &capturingDiagnosticLogger{}

    installBunDiagnostics(logger)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    records := logger.captured()
    if 0 == len(records) {
        t.Fatal("bun's own diagnostic did not reach the journal")
    }

    if "bun diagnostic" != records[0].message {
        t.Fatalf("message = %q, want %q", records[0].message, "bun diagnostic")
    }

    if loggingcontract.LevelWarning != records[0].level {
        t.Fatalf("level = %v, want warning", records[0].level)
    }

    line, present := records[0].context["line"]
    if false == present {
        t.Fatalf("the record does not carry the line: %v", records[0].context)
    }

    if false == strings.Contains(fmt.Sprintf("%v", line), "placeholders") {
        t.Fatalf("line = %v, want bun's own wording", line)
    }
}

/* a provider that could not resolve a logger takes nothing on the way past: without the guard it would install an adapter over nothing, and bun's diagnostics would go from standard error to nowhere at all — a destination strictly worse than the one it replaced. */
func TestRouteDiagnostics_ANilLoggerLeavesTheDestinationWhereItWas(t *testing.T) {
    logger := &capturingDiagnosticLogger{}
    installBunDiagnostics(logger)

    RouteDiagnostics(nil)

    _ = schema.SafeQuery("SELECT 1", []any{42})

    if 0 == len(logger.captured()) {
        t.Fatal("a nil logger replaced the destination that was already installed")
    }
}
