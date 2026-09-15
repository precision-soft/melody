package logging

import (
    "io"
    "os"
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func TestNewRequestLogger_PanicsWhenBaseLoggerIsNil(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRequestLogger(nil, "r1", "requestId")
        },
        "base logger is not provided for request logger",
    )
}

func TestNewRequestLogger_PanicsWhenBaseLoggerIsATypedNil(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRequestLogger((*jsonLogger)(nil), "r1", "requestId")
        },
        "base logger is not provided for request logger",
    )
}

func TestNewRequestLogger_PanicsWhenContextKeyIsEmpty(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRequestLogger(&captureLogger{}, "r1", "")
        },
        "invalid context key for request logger",
    )
}

func TestNewRequestLogger_ReturnsBaseWhenRequestIdIsEmpty(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "", "requestId")

    if logger != base {
        t.Fatalf("expected base logger")
    }
}

func TestRequestLogger_AddsRequestIdWhenMissing(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info("msg", map[string]any{"a": "b"})

    if 1 != base.calls {
        t.Fatalf("expected one call")
    }

    if "r1" != base.lastContext["requestId"] {
        t.Fatalf("expected requestId to be injected")
    }

    if "b" != base.lastContext["a"] {
        t.Fatalf("expected context to be preserved")
    }
}

func TestRequestLogger_RealRequestIdWinsAndKeepsTheClaim(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info(
        "msg",
        map[string]any{
            "requestId": "existing",
        },
    )

    if "r1" != base.lastContext["requestId"] {
        t.Fatalf("expected the real request id under the key, got %v", base.lastContext["requestId"])
    }

    if "existing" != base.lastContext["requestIdClaimed"] {
        t.Fatalf("expected the displaced claim to be preserved, got %v", base.lastContext["requestIdClaimed"])
    }
}

func TestRequestLogger_EqualClaimAddsNoClaimedKey(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info(
        "msg",
        map[string]any{
            "requestId": "r1",
        },
    )

    if "r1" != base.lastContext["requestId"] {
        t.Fatalf("expected the request id under the key")
    }

    if _, hasClaim := base.lastContext["requestIdClaimed"]; true == hasClaim {
        t.Fatalf("expected no claimed key for an equal claim")
    }
}

func TestRequestLogger_OverridesExistingEmptyRequestId(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info(
        "msg",
        map[string]any{
            "requestId": "",
        },
    )

    if "r1" != base.lastContext["requestId"] {
        t.Fatalf("expected empty requestId to be replaced")
    }
}

func TestRequestLogger_ClosedForwardsToTheBase(t *testing.T) {
    file, createErr := os.CreateTemp(t.TempDir(), "melody-request-logger-*.log")
    if nil != createErr {
        t.Fatalf("unexpected temp file error: %v", createErr)
    }

    baseLogger := NewJsonLogger(file, loggingcontract.LevelInfo)
    wrappedLogger := NewRequestLogger(baseLogger, "request-id", "requestId")

    closedChecker, isChecker := wrappedLogger.(interface{ Closed() bool })
    if false == isChecker {
        t.Fatalf("expected the request logger to forward the closed question")
    }

    if true == closedChecker.Closed() {
        t.Fatalf("expected an open base to report open through the wrapper")
    }

    baseCloser := baseLogger.(interface{ Close() error })
    if closeErr := baseCloser.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == closedChecker.Closed() {
        t.Fatalf("expected the closed base to report closed through the wrapper")
    }
}

func TestRequestLogger_EveryMethodMergesTheRequestIdAndKeepsTheLevel(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    methodList := []struct {
        name          string
        call          func()
        expectedLevel loggingcontract.Level
    }{
        {"Log", func() { logger.Log(loggingcontract.LevelWarning, "msg", map[string]any{"a": "b"}) }, loggingcontract.LevelWarning},
        {"Debug", func() { logger.Debug("msg", map[string]any{"a": "b"}) }, loggingcontract.LevelDebug},
        {"Info", func() { logger.Info("msg", map[string]any{"a": "b"}) }, loggingcontract.LevelInfo},
        {"Warning", func() { logger.Warning("msg", map[string]any{"a": "b"}) }, loggingcontract.LevelWarning},
        {"Error", func() { logger.Error("msg", map[string]any{"a": "b"}) }, loggingcontract.LevelError},
        {"Emergency", func() { logger.Emergency("msg", map[string]any{"a": "b"}) }, loggingcontract.LevelEmergency},
    }

    for _, methodEntry := range methodList {
        methodEntry.call()

        if methodEntry.expectedLevel != base.lastLevel {
            t.Fatalf("%s: expected the base to be called at level %q, got %q", methodEntry.name, methodEntry.expectedLevel, base.lastLevel)
        }

        if "r1" != base.lastContext["requestId"] {
            t.Fatalf("%s: expected the request id to be merged, got %v", methodEntry.name, base.lastContext["requestId"])
        }

        if "b" != base.lastContext["a"] {
            t.Fatalf("%s: expected the caller's context to survive, got %v", methodEntry.name, base.lastContext)
        }
    }

    if len(methodList) != base.calls {
        t.Fatalf("expected one base call per method, got %d", base.calls)
    }
}

func TestRequestLogger_DoesNotWriteIntoTheCallersContext(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    callerContext := map[string]any{"a": "b"}

    logger.Info("msg", callerContext)

    if _, exists := callerContext["requestId"]; true == exists {
        t.Fatalf("expected the caller's context to stay untouched, got %v", callerContext)
    }
}

func TestRequestLogger_NilContext_StillCarriesTheRequestId(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info("msg", nil)

    if "r1" != base.lastContext["requestId"] {
        t.Fatalf("expected the request id under the key, got %v", base.lastContext)
    }
}

func TestRequestLogger_NonStringClaim_IsReplacedWithoutAClaimedKey(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info("msg", map[string]any{"requestId": 42})

    if "r1" != base.lastContext["requestId"] {
        t.Fatalf("expected the real request id under the key, got %v", base.lastContext["requestId"])
    }

    if _, hasClaim := base.lastContext["requestIdClaimed"]; true == hasClaim {
        t.Fatalf("expected no claimed key for a non-string value, got %v", base.lastContext["requestIdClaimed"])
    }
}

func TestRequestLogger_ClosedReportsOpenForABaseWithoutTheQuestion(t *testing.T) {
    wrappedLogger := NewRequestLogger(&captureLogger{}, "request-id", "requestId")

    closedChecker, isChecker := wrappedLogger.(interface{ Closed() bool })
    if false == isChecker {
        t.Fatalf("expected the request logger to answer the closed question")
    }

    if true == closedChecker.Closed() {
        t.Fatalf("expected a base without the question to be reported open")
    }
}

func TestRequestLogger_MergeWithAnEmptyRequestId_LeavesTheContextAlone(t *testing.T) {
    instance := &requestLogger{
        base:       &captureLogger{},
        requestId:  "",
        contextKey: "requestId",
    }

    callerContext := loggingcontract.Context{"a": "b"}

    merged := instance.mergeContextWithRequestId(callerContext, "")

    if _, exists := merged["requestId"]; true == exists {
        t.Fatalf("expected no request id to be written, got %v", merged)
    }

    if "b" != merged["a"] {
        t.Fatalf("expected the caller's context to be returned, got %v", merged)
    }
}

func TestProcessLogger_TheGeneratedIdWinsAndTheCallerValueSurvivesUnderProvided(t *testing.T) {
    base := &captureLogger{}
    wrappedLogger := NewProcessLogger(base, "process-id", "processId")

    wrappedLogger.Info("spawned a child", loggingcontract.Context{"processId": "child-pid-4242"})

    if "process-id" != base.lastContext["processId"] {
        t.Fatalf("expected the generated id to win the key, got %v", base.lastContext["processId"])
    }

    if "child-pid-4242" != base.lastContext["processIdProvided"] {
        t.Fatalf("expected the caller's value under the provided key, got %v", base.lastContext["processIdProvided"])
    }

    if _, hasClaim := base.lastContext["processIdClaimed"]; true == hasClaim {
        t.Fatalf("expected the console path to write no claimed key, got %v", base.lastContext["processIdClaimed"])
    }
}

func TestProcessLogger_PreservesANonStringCallerValueVerbatim(t *testing.T) {
    base := &captureLogger{}
    wrappedLogger := NewProcessLogger(base, "process-id", "processId")

    wrappedLogger.Info("spawned a child", loggingcontract.Context{"processId": 4242})

    if "process-id" != base.lastContext["processId"] {
        t.Fatalf("expected the generated id to win the key, got %v", base.lastContext["processId"])
    }

    if 4242 != base.lastContext["processIdProvided"] {
        t.Fatalf("expected the non-string caller value verbatim under the provided key, got %v", base.lastContext["processIdProvided"])
    }
}

func TestProcessLogger_AnEqualCallerValueAddsNoProvidedKey(t *testing.T) {
    base := &captureLogger{}
    wrappedLogger := NewProcessLogger(base, "process-id", "processId")

    wrappedLogger.Info("plain record", loggingcontract.Context{"processId": "process-id"})

    if "process-id" != base.lastContext["processId"] {
        t.Fatalf("expected the id under the key, got %v", base.lastContext["processId"])
    }

    if _, hasProvided := base.lastContext["processIdProvided"]; true == hasProvided {
        t.Fatalf("expected no provided key for an equal value, got %v", base.lastContext["processIdProvided"])
    }
}

func TestProcessLogger_AnAbsentCallerKeyGetsOnlyTheId(t *testing.T) {
    base := &captureLogger{}
    wrappedLogger := NewProcessLogger(base, "process-id", "processId")

    wrappedLogger.Info("plain record", loggingcontract.Context{"jobId": "job-42"})

    if "process-id" != base.lastContext["processId"] {
        t.Fatalf("expected the id under the key, got %v", base.lastContext["processId"])
    }

    if _, hasProvided := base.lastContext["processIdProvided"]; true == hasProvided {
        t.Fatalf("expected no provided key when the caller wrote none, got %v", base.lastContext["processIdProvided"])
    }
}

func TestProcessLogger_AnEmptyProcessIdReturnsTheBaseUndecorated(t *testing.T) {
    base := &captureLogger{}

    wrappedLogger := NewProcessLogger(base, "", "processId")

    if wrappedLogger != loggingcontract.Logger(base) {
        t.Fatalf("expected the base logger back for an empty process id")
    }
}

func TestRequestLogger_EnabledForwardsToTheBase(t *testing.T) {
    baseLogger := NewJsonLogger(io.Discard, loggingcontract.LevelError)
    wrappedLogger := NewRequestLogger(baseLogger, "request-id", "requestId")

    levelReporter, isReporter := wrappedLogger.(loggingcontract.LevelReporter)
    if false == isReporter {
        t.Fatalf("expected the request logger to answer the level question")
    }

    if true == levelReporter.Enabled(loggingcontract.LevelDebug) {
        t.Fatalf("expected debug to be reported disabled through a base configured at error")
    }

    if false == levelReporter.Enabled(loggingcontract.LevelError) {
        t.Fatalf("expected error to be reported enabled through a base configured at error")
    }
}

func TestRequestLogger_EnabledReportsEnabledOverABaseWithoutTheCapability(t *testing.T) {
    wrappedLogger := NewRequestLogger(&captureLogger{}, "request-id", "requestId")

    levelReporter := wrappedLogger.(loggingcontract.LevelReporter)

    if false == levelReporter.Enabled(loggingcontract.LevelDebug) {
        t.Fatalf("expected a base without the capability to be reported enabled")
    }
}
