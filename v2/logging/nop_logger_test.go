package logging

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

/* a typed nil stored in the interface is not equal to nil, so the plain nil check let it through and the first call panicked on the exact path this function exists to guard */
func TestEnsureLogger_ReplacesATypedNilLogger(t *testing.T) {
    var typedNil *jsonLogger

    ensured := EnsureLogger(typedNil)

    ensured.Info("message", nil)

    if _, isNop := ensured.(*nopLogger); false == isNop {
        t.Fatalf("expected a typed nil to be replaced by the nop logger, got %T", ensured)
    }
}

func TestEnsureLogger_KeepsAUsableLogger(t *testing.T) {
    logger := NewNopLogger()

    if logger != EnsureLogger(logger) {
        t.Fatalf("expected a usable logger to be kept")
    }
}

/* the six methods of the no-op logger have empty bodies, so no test can move their coverage — a statement-free function reports 0.0% for ever. What can be pinned is the contract they exist for: this is the logger EnsureLogger substitutes for a missing one, and every level must be swallowed, including a nil context, rather than panicking inside a fallback that exists so nothing panics */
func TestNopLogger_SwallowsEveryLevel(t *testing.T) {
    logger := NewNopLogger()

    logger.Log(loggingcontract.LevelWarning, "message", loggingcontract.Context{"key": "value"})
    logger.Debug("message", nil)
    logger.Info("message", nil)
    logger.Warning("message", nil)
    logger.Error("message", nil)
    logger.Emergency("message", nil)
}

/* the substitute must not claim it can be closed or that it is closed: the exit handler asks the question before trusting a logger with the final record, and a no-op answering "closed" would send that record to the emergency logger for no reason */
func TestNopLogger_DoesNotAnswerTheClosedQuestion(t *testing.T) {
    logger := NewNopLogger()

    if _, isChecker := logger.(interface{ Closed() bool }); true == isChecker {
        t.Fatalf("expected the no-op logger not to answer the closed question")
    }
}

/* the substitute reports nothing enabled, which is the whole of its answer: it is the one logger for which the capability is not a threshold but a statement about every level. A caller that builds its records eagerly — the event dispatcher assembling a context map per dispatch — used to pay for every one of them against a logger that exists to accept and forget. */
func TestNopLogger_ReportsNoLevelEnabled(t *testing.T) {
    logger := NewNopLogger()

    levelReporter, isReporter := logger.(loggingcontract.LevelReporter)
    if false == isReporter {
        t.Fatalf("expected the no-op logger to answer the level question")
    }

    for _, level := range []loggingcontract.Level{
        loggingcontract.LevelDebug,
        loggingcontract.LevelInfo,
        loggingcontract.LevelWarning,
        loggingcontract.LevelError,
        loggingcontract.LevelEmergency,
    } {
        if true == levelReporter.Enabled(level) {
            t.Fatalf("expected %q to be reported disabled by the no-op logger", level)
        }
    }
}
