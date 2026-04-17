package exception

import (
    "errors"
    "fmt"
    "testing"
)

func TestBuildCauseChain_NilReturnsNil(t *testing.T) {
    chain := BuildCauseChain(nil, 8)

    if nil != chain {
        t.Fatalf("expected nil for nil error")
    }
}

func TestBuildCauseChain_SingleErrorReturnsOneElement(t *testing.T) {
    causeErr := errors.New("single cause")

    chain := BuildCauseChain(causeErr, 8)

    if 1 != len(chain) {
        t.Fatalf("expected 1 element, got: %d", len(chain))
    }

    if "single cause" != chain[0] {
        t.Fatalf("expected chain[0] to be 'single cause', got: %s", chain[0])
    }
}

func TestBuildCauseChain_WrappedErrorsUnwrapCorrectly(t *testing.T) {
    rootCause := errors.New("root cause")
    middleCause := fmt.Errorf("middle: %w", rootCause)
    topCause := fmt.Errorf("top: %w", middleCause)

    chain := BuildCauseChain(topCause, 8)

    if 3 != len(chain) {
        t.Fatalf("expected 3 elements, got: %d", len(chain))
    }

    if "top: middle: root cause" != chain[0] {
        t.Fatalf("unexpected chain[0]: %s", chain[0])
    }

    if "middle: root cause" != chain[1] {
        t.Fatalf("unexpected chain[1]: %s", chain[1])
    }

    if "root cause" != chain[2] {
        t.Fatalf("unexpected chain[2]: %s", chain[2])
    }
}

func TestBuildCauseChain_RespectsMaxDepth(t *testing.T) {
    rootCause := errors.New("root")
    middleCause := fmt.Errorf("middle: %w", rootCause)
    topCause := fmt.Errorf("top: %w", middleCause)

    chain := BuildCauseChain(topCause, 2)

    if 2 != len(chain) {
        t.Fatalf("expected 2 elements (maxDepth=2), got: %d", len(chain))
    }
}

func TestBuildCauseChain_ZeroMaxDepthReturnsSingleElement(t *testing.T) {
    causeErr := errors.New("cause")

    chain := BuildCauseChain(causeErr, 0)

    if 1 != len(chain) {
        t.Fatalf("expected 1 element for maxDepth=0, got: %d", len(chain))
    }
}

func TestBuildCauseChain_WithExceptionError(t *testing.T) {
    innerErr := NewError("inner error", map[string]any{"key": "value"}, nil)
    outerErr := NewError("outer error", nil, innerErr)

    chain := BuildCauseChain(outerErr, 8)

    if 2 != len(chain) {
        t.Fatalf("expected 2 elements, got: %d", len(chain))
    }

    if "outer error" != chain[0] {
        t.Fatalf("unexpected chain[0]: %s", chain[0])
    }

    if "inner error" != chain[1] {
        t.Fatalf("unexpected chain[1]: %s", chain[1])
    }
}

func TestBuildCauseContextChain_NilReturnsNil(t *testing.T) {
    chain := BuildCauseContextChain(nil, 8)

    if nil != chain {
        t.Fatalf("expected nil for nil error")
    }
}

func TestBuildCauseContextChain_ReturnsNilWhenNoContextExists(t *testing.T) {
    causeErr := errors.New("plain error")

    chain := BuildCauseContextChain(causeErr, 8)

    if nil != chain {
        t.Fatalf("expected nil when no context exists, got: %v", chain)
    }
}

func TestBuildCauseContextChain_ReturnsContextFromExceptionErrors(t *testing.T) {
    innerErr := NewError("inner", map[string]any{"innerKey": "innerValue"}, nil)
    outerErr := NewError("outer", map[string]any{"outerKey": "outerValue"}, innerErr)

    chain := BuildCauseContextChain(outerErr, 8)

    if nil == chain {
        t.Fatalf("expected non-nil chain")
    }

    if 2 != len(chain) {
        t.Fatalf("expected 2 elements, got: %d", len(chain))
    }

    if "outerValue" != chain[0]["outerKey"] {
        t.Fatalf("unexpected outer context: %v", chain[0])
    }

    if "innerValue" != chain[1]["innerKey"] {
        t.Fatalf("unexpected inner context: %v", chain[1])
    }
}

func TestLogContext_AddsChainFieldsFromCause(t *testing.T) {
    innerErr := NewError("inner", map[string]any{"innerKey": "v"}, nil)
    outerErr := NewError("outer", nil, innerErr)

    context := LogContext(outerErr)

    if nil == context {
        t.Fatalf("expected non-nil context")
    }

    causeValue, hasCause := context["cause"]
    if false == hasCause {
        t.Fatalf("expected cause field in context")
    }

    causeString, ok := causeValue.(string)
    if false == ok {
        t.Fatalf("expected cause to be string, got %T", causeValue)
    }

    if "inner" != causeString {
        t.Fatalf("unexpected cause: %s", causeString)
    }

    causeChainValue, hasCauseChain := context["causeChain"]
    if false == hasCauseChain {
        t.Fatalf("expected causeChain field in context")
    }

    causeChainSlice, ok := causeChainValue.([]string)
    if false == ok {
        t.Fatalf("expected causeChain to be []string, got %T", causeChainValue)
    }

    if 1 != len(causeChainSlice) {
        t.Fatalf("expected causeChain to have 1 element, got %d", len(causeChainSlice))
    }
}

func TestLogContext_NilErrorWithExtraContext(t *testing.T) {
    context := LogContext(nil, map[string]any{"key": "value"})

    if nil == context {
        t.Fatalf("expected non-nil context")
    }

    if "value" != context["key"] {
        t.Fatalf("expected key=value in context")
    }
}

func TestLogContext_NilErrorNilExtra(t *testing.T) {
    context := LogContext(nil)

    if nil != context {
        t.Fatalf("expected nil context")
    }
}
