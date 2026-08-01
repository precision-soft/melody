package logging

import (
    "os"
    "testing"

    "github.com/precision-soft/melody/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* @info the decorator dereferences its base on every single call, so a missing base is refused where the wiring mistake is, not on the first record of the first request. The assertion is on the message: a value-blind one passes over any panic, including one thrown for an unrelated reason */
func TestNewRequestLogger_PanicsWhenBaseLoggerIsNil(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRequestLogger(nil, "r1", "requestId")
        },
        "base logger is not provided for request logger",
    )
}

/* @info a typed-nil base passes the plain nil comparison and dereferences the nil receiver on the first record — the same shape the package refuses everywhere else, and the reason the guard reads through the interface instead of comparing to nil */
func TestNewRequestLogger_PanicsWhenBaseLoggerIsATypedNil(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRequestLogger((*jsonLogger)(nil), "r1", "requestId")
        },
        "base logger is not provided for request logger",
    )
}

/* @info an empty context key would write the request id under "", where nothing reads it, and the claim-preserving key would become "Claimed" — the correlation the decorator exists to add would be silently unreachable */
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

/* @info deliberate contract change: the real request id wins the key unconditionally — a value already under it frequently originates in an error context assembled from request data, and letting it win let client data forge the record's correlation. The displaced claim is kept under the key suffixed "Claimed", so the record carries both the truth and the claim. */
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

/* @info a claim equal to the real id is not an impersonation and earns no extra key */
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

/* @info the exit handler refuses a logger that reports itself closed; a decorator that cannot answer hid a dead file logger behind a live-looking wrapper, and the final record was handed to it and dropped — the wrapper now forwards the question to the base it decorates */
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

/* @info all six methods of the decorator merge the id, and each is its own delegation: only Info had ever been entered, so a method wired to the wrong base call — or forgetting the merge — would drop the correlation of every record written through it. The level the base receives says which delegation ran */
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

/* @info the merge copies rather than writing into the caller's map: the context frequently belongs to an error that outlives the record, and stamping a request id into it would follow that error into every other record it reaches */
func TestRequestLogger_DoesNotWriteIntoTheCallersContext(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    callerContext := map[string]any{"a": "b"}

    logger.Info("msg", callerContext)

    if _, exists := callerContext["requestId"]; true == exists {
        t.Fatalf("expected the caller's context to stay untouched, got %v", callerContext)
    }
}

/* @info a nil context still receives the id: the record of a failure with no context of its own is exactly the one an operator correlates by id */
func TestRequestLogger_NilContext_StillCarriesTheRequestId(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info("msg", nil)

    if "r1" != base.lastContext["requestId"] {
        t.Fatalf("expected the request id under the key, got %v", base.lastContext)
    }
}

/* @info a claim that is not a string is not a competing request id — it is a value that happens to sit under the key — so it is overwritten without earning the Claimed key that exists to preserve a rival spelling */
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

/* @info a base that does not answer the question is reported open, exactly as the exit handler treats such a logger when asked directly */
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

/* @info the merge answers the caller's own context untouched for an empty id. The constructor never builds a decorator with one — it hands the base back instead — so the branch is reachable only from a struct built by hand; it is the guard that keeps a decorator assembled outside the constructor from writing an empty correlation over a real one */
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
