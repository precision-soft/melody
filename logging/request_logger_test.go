package logging

import (
    "testing"

    "github.com/precision-soft/melody/exception"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type captureLogger struct {
    lastLevel   loggingcontract.Level
    lastMessage string
    lastContext map[string]any
    calls       int
}

func (instance *captureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.calls++
    instance.lastLevel = level
    instance.lastMessage = message
    instance.lastContext = context
}

func (instance *captureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *captureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *captureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *captureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *captureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

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

var _ loggingcontract.Logger = (*captureLogger)(nil)
var _ = exception.NewError
