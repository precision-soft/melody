package logging

import (
    "os"
    "testing"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func TestNewRequestLogger_PanicsWhenBaseLoggerIsNil(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewRequestLogger(nil, "r1", "requestId")
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
