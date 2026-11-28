package logging

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

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

    for _, level := range []loggingcontract.Level{
        loggingcontract.LevelDebug,
        loggingcontract.LevelInfo,
        loggingcontract.LevelWarning,
        loggingcontract.LevelError,
        loggingcontract.LevelEmergency,
    } {
        if true == logger.Enabled(level) {
            t.Fatalf("expected %q to be reported disabled by the no-op logger", level)
        }
    }
}

func TestNopLogger_CauseChainDepthAnswersTheDefault(t *testing.T) {
    if causeChainDefaultDepth != NewNopLogger().CauseChainDepth() {
        t.Fatalf("expected the no-op logger to answer the default depth, got %d", NewNopLogger().CauseChainDepth())
    }
}
