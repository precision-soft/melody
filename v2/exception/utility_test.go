package exception

import (
	"errors"
	"fmt"
	"testing"
)

func TestBuildCauseChain_NilError_ReturnsNil(t *testing.T) {
	chain := BuildCauseChain(nil, 8)
	if nil != chain {
		t.Fatalf("expected nil chain for nil error")
	}
}

func TestBuildCauseChain_SingleError_ReturnsSingleElement(t *testing.T) {
	causeErr := errors.New("root cause")

	chain := BuildCauseChain(causeErr, 8)
	if 1 != len(chain) {
		t.Fatalf("expected chain length 1, got %d", len(chain))
	}

	if "root cause" != chain[0] {
		t.Fatalf("unexpected chain value: %s", chain[0])
	}
}

func TestBuildCauseChain_WrappedErrors_WalksChain(t *testing.T) {
	rootErr := errors.New("root")
	wrappedErr := fmt.Errorf("middle: %w", rootErr)
	outerErr := fmt.Errorf("outer: %w", wrappedErr)

	chain := BuildCauseChain(outerErr, 8)
	if 3 != len(chain) {
		t.Fatalf("expected chain length 3, got %d", len(chain))
	}

	if "outer: middle: root" != chain[0] {
		t.Fatalf("unexpected first element: %s", chain[0])
	}

	if "middle: root" != chain[1] {
		t.Fatalf("unexpected second element: %s", chain[1])
	}

	if "root" != chain[2] {
		t.Fatalf("unexpected third element: %s", chain[2])
	}
}

func TestBuildCauseChain_RespectsMaxDepth(t *testing.T) {
	rootErr := errors.New("root")
	wrappedErr := fmt.Errorf("middle: %w", rootErr)
	outerErr := fmt.Errorf("outer: %w", wrappedErr)

	chain := BuildCauseChain(outerErr, 2)
	if 2 != len(chain) {
		t.Fatalf("expected chain length 2, got %d", len(chain))
	}
}

func TestBuildCauseChain_ZeroMaxDepth_ReturnsSingleElement(t *testing.T) {
	causeErr := errors.New("root")

	chain := BuildCauseChain(causeErr, 0)
	if 1 != len(chain) {
		t.Fatalf("expected chain length 1, got %d", len(chain))
	}
}

func TestBuildCauseChain_ExceptionError_UnwrapsCorrectly(t *testing.T) {
	rootErr := errors.New("base cause")
	exceptionErr := NewError("wrapped exception", nil, rootErr)

	chain := BuildCauseChain(exceptionErr, 8)
	if 2 != len(chain) {
		t.Fatalf("expected chain length 2, got %d", len(chain))
	}

	if "wrapped exception" != chain[0] {
		t.Fatalf("unexpected first element: %s", chain[0])
	}

	if "base cause" != chain[1] {
		t.Fatalf("unexpected second element: %s", chain[1])
	}
}

func TestBuildCauseContextChain_NilError_ReturnsNil(t *testing.T) {
	chain := BuildCauseContextChain(nil, 8)
	if nil != chain {
		t.Fatalf("expected nil chain for nil error")
	}
}

func TestBuildCauseContextChain_ErrorWithNoContext_ReturnsNil(t *testing.T) {
	causeErr := errors.New("plain error")

	chain := BuildCauseContextChain(causeErr, 8)
	if nil != chain {
		t.Fatalf("expected nil chain when no exception has context")
	}
}

func TestBuildCauseContextChain_ExceptionWithContext_ReturnsChain(t *testing.T) {
	rootErr := NewError("root", map[string]any{"rootKey": "rootValue"}, nil)
	wrappedErr := NewError("wrapped", map[string]any{"wrappedKey": "wrappedValue"}, rootErr)

	chain := BuildCauseContextChain(wrappedErr, 8)
	if nil == chain {
		t.Fatalf("expected non-nil chain")
	}

	if 2 != len(chain) {
		t.Fatalf("expected chain length 2, got %d", len(chain))
	}

	if nil == chain[0] {
		t.Fatalf("expected first entry to have context")
	}

	if "wrappedValue" != chain[0]["wrappedKey"] {
		t.Fatalf("unexpected first context value")
	}

	if nil == chain[1] {
		t.Fatalf("expected second entry to have context")
	}

	if "rootValue" != chain[1]["rootKey"] {
		t.Fatalf("unexpected second context value")
	}
}

func TestBuildCauseContextChain_MixedErrorTypes_IncludesNilForPlainErrors(t *testing.T) {
	plainErr := errors.New("plain")
	exceptionErr := NewError("exception", map[string]any{"key": "value"}, plainErr)

	chain := BuildCauseContextChain(exceptionErr, 8)
	if nil == chain {
		t.Fatalf("expected non-nil chain")
	}

	if 2 != len(chain) {
		t.Fatalf("expected chain length 2, got %d", len(chain))
	}

	if nil == chain[0] {
		t.Fatalf("expected first entry to have context")
	}

	if nil != chain[1] {
		t.Fatalf("expected second entry to be nil for plain error")
	}
}

func TestLogContext_NilError_ReturnsNil(t *testing.T) {
	context := LogContext(nil)
	if nil != context {
		t.Fatalf("expected nil context for nil error")
	}
}

func TestLogContext_NilErrorWithExtra_ReturnsMerged(t *testing.T) {
	extra := map[string]any{"key": "value"}
	context := LogContext(nil, extra)
	if nil == context {
		t.Fatalf("expected non-nil context")
	}

	if "value" != context["key"] {
		t.Fatalf("unexpected context value")
	}
}

func TestLogContext_ExceptionErrorWithCause_IncludesCauseChain(t *testing.T) {
	rootErr := errors.New("root cause")
	exceptionErr := NewError("main error", nil, rootErr)

	context := LogContext(exceptionErr)
	if nil == context {
		t.Fatalf("expected non-nil context")
	}

	if "main error" != context["error"] {
		t.Fatalf("unexpected error value: %v", context["error"])
	}

	causeValue, hasCause := context["cause"]
	if false == hasCause {
		t.Fatalf("expected cause in context")
	}
	if "root cause" != causeValue {
		t.Fatalf("unexpected cause value: %v", causeValue)
	}

	causeChainValue, hasCauseChain := context["causeChain"]
	if false == hasCauseChain {
		t.Fatalf("expected causeChain in context")
	}

	causeChain, ok := causeChainValue.([]string)
	if false == ok {
		t.Fatalf("expected causeChain to be []string")
	}
	if 1 != len(causeChain) {
		t.Fatalf("expected causeChain length 1, got %d", len(causeChain))
	}
}

func TestLogContext_PlainError_SetsErrorField(t *testing.T) {
	plainErr := errors.New("plain error")

	context := LogContext(plainErr)
	if nil == context {
		t.Fatalf("expected non-nil context")
	}

	if "plain error" != context["error"] {
		t.Fatalf("unexpected error value: %v", context["error"])
	}
}

func TestLogContext_WithExtra_MergesExtraIntoContext(t *testing.T) {
	exceptionErr := NewError("msg", map[string]any{"existing": "yes"}, nil)
	extra := map[string]any{"extra": "value"}

	context := LogContext(exceptionErr, extra)
	if nil == context {
		t.Fatalf("expected non-nil context")
	}

	if "yes" != context["existing"] {
		t.Fatalf("expected existing context to be preserved")
	}

	if "value" != context["extra"] {
		t.Fatalf("expected extra context to be merged")
	}
}
