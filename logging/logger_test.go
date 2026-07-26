package logging

import (
    "bytes"
    "errors"
    "log"
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
)

func TestLogError_NilLogger_DoesNotPrintEmptyContext(t *testing.T) {
    var buffer bytes.Buffer

    originalWriter := log.Writer()
    log.SetOutput(&buffer)
    defer func() {
        log.SetOutput(originalWriter)
    }()

    err := exception.NewError("message", nil, nil)

    LogError(nil, err)

    output := buffer.String()

    if false == strings.Contains(output, "message") {
        t.Fatalf("expected message in output")
    }

    if true == strings.Contains(output, "context=") {
        t.Fatalf("did not expect context output for empty context")
    }
}

func TestEnrichContextWithCause_NoCause_ReturnsOriginalContext(t *testing.T) {
    exceptionValue := exception.NewError("msg", map[string]any{"key": "value"}, nil)

    enrichedContext := enrichContextWithCause(exceptionValue)

    if "value" != enrichedContext["key"] {
        t.Fatalf("expected original context to be preserved")
    }

    _, hasCause := enrichedContext["cause"]
    if true == hasCause {
        t.Fatalf("expected no cause when causeErr is nil")
    }
}

func TestEnrichContextWithCause_WithCause_AddsCauseAndCauseChain(t *testing.T) {
    rootErr := errors.New("root cause")
    exceptionValue := exception.NewError("msg", nil, rootErr)

    enrichedContext := enrichContextWithCause(exceptionValue)

    causeValue, hasCause := enrichedContext["cause"]
    if false == hasCause {
        t.Fatalf("expected cause to be present")
    }

    if "root cause" != causeValue {
        t.Fatalf("unexpected cause value: %v", causeValue)
    }

    causeChainValue, hasCauseChain := enrichedContext["causeChain"]
    if false == hasCauseChain {
        t.Fatalf("expected causeChain to be present")
    }

    causeChain, ok := causeChainValue.([]string)
    if false == ok {
        t.Fatalf("expected causeChain to be []string")
    }

    if 1 != len(causeChain) {
        t.Fatalf("expected causeChain length 1, got %d", len(causeChain))
    }

    if "root cause" != causeChain[0] {
        t.Fatalf("unexpected causeChain value: %s", causeChain[0])
    }
}

func TestEnrichContextWithCause_NestedCause_BuildsFullChain(t *testing.T) {
    rootErr := errors.New("root")
    middleErr := exception.NewError("middle", map[string]any{"middleKey": "middleValue"}, rootErr)
    outerErr := exception.NewError("outer", nil, middleErr)

    enrichedContext := enrichContextWithCause(outerErr)

    causeChainValue, hasCauseChain := enrichedContext["causeChain"]
    if false == hasCauseChain {
        t.Fatalf("expected causeChain to be present")
    }

    causeChain, ok := causeChainValue.([]string)
    if false == ok {
        t.Fatalf("expected causeChain to be []string")
    }

    if 2 != len(causeChain) {
        t.Fatalf("expected the cause chain to carry the middle and root causes, got %v", causeChain)
    }

    if "middle" != causeChain[0] || "root" != causeChain[1] {
        t.Fatalf("expected the cause chain to name the middle and root causes in order, got %v", causeChain)
    }

    causeContextChainValue, hasCauseContextChain := enrichedContext["causeContextChain"]
    if false == hasCauseContextChain {
        t.Fatalf("expected causeContextChain to be present")
    }

    causeContextChain, ok := causeContextChainValue.([]map[string]any)
    if false == ok {
        t.Fatalf("expected causeContextChain to be []map[string]any")
    }

    if 0 == len(causeContextChain) {
        t.Fatalf("expected causeContextChain to have entries")
    }
}

func TestLogError_NilError_DoesNothing(t *testing.T) {
    var buffer bytes.Buffer

    originalWriter := log.Writer()
    log.SetOutput(&buffer)
    defer func() {
        log.SetOutput(originalWriter)
    }()

    LogError(nil, nil)

    if 0 != buffer.Len() {
        t.Fatalf("expected no output for nil error")
    }
}

func TestLogError_PlainError_NilLogger_PrintsError(t *testing.T) {
    var buffer bytes.Buffer

    originalWriter := log.Writer()
    log.SetOutput(&buffer)
    defer func() {
        log.SetOutput(originalWriter)
    }()

    LogError(nil, errors.New("plain error"))

    output := buffer.String()
    if false == strings.Contains(output, "plain error") {
        t.Fatalf("expected plain error in output, got: %s", output)
    }
}
