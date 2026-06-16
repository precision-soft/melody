package logging

import (
    "testing"
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

func TestRequestLogger_DoesNotOverrideExistingNonEmptyRequestId(t *testing.T) {
    base := &captureLogger{}

    logger := NewRequestLogger(base, "r1", "requestId")

    logger.Info(
        "msg",
        map[string]any{
            "requestId": "existing",
        },
    )

    if "existing" != base.lastContext["requestId"] {
        t.Fatalf("expected existing requestId to remain")
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
