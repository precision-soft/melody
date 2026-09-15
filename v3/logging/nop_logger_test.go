package logging

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

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

func TestNopLogger_SwallowsEveryLevel(t *testing.T) {
    logger := NewNopLogger()

    logger.Log(loggingcontract.LevelWarning, "message", loggingcontract.Context{"key": "value"})
    logger.Debug("message", nil)
    logger.Info("message", nil)
    logger.Warning("message", nil)
    logger.Error("message", nil)
    logger.Emergency("message", nil)
}

func TestNopLogger_DoesNotAnswerTheClosedQuestion(t *testing.T) {
    logger := NewNopLogger()

    if _, isChecker := logger.(interface{ Closed() bool }); true == isChecker {
        t.Fatalf("expected the no-op logger not to answer the closed question")
    }
}

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
