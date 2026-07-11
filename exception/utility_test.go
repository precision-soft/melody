package exception

import (
    "errors"
    "math"
    "testing"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func TestFromError_ReturnsNilOnNil(t *testing.T) {
    if nil != FromError(nil) {
        t.Fatalf("expected nil")
    }
}

func TestFromError_ReturnsSameWhenAlreadyException(t *testing.T) {
    expected := NewError("x", nil, nil)

    if expected != FromError(expected) {
        t.Fatalf("expected same instance")
    }
}

func TestFromError_WrapsNonExceptionError(t *testing.T) {
    base := errors.New("base")

    ex := FromError(base)
    if nil == ex {
        t.Fatalf("expected *Error")
    }

    if base.Error() != ex.Message() {
        t.Fatalf("expected message to equal base error string")
    }

    if base != ex.CauseErr() {
        t.Fatalf("expected cause to be base error")
    }

    if loggingcontract.LevelError != ex.Level() {
        t.Fatalf("expected default level error")
    }
}

/* @info regression: unclamped huge maxDepth must not drive the up-front allocation */

func TestBuildCauseChain_HugeMaxDepthDoesNotPanic(t *testing.T) {
    causeErr := errors.New("cause")

    chain := BuildCauseChain(causeErr, math.MaxInt)

    if 1 != len(chain) {
        t.Fatalf("expected 1 element for single-link chain, got: %d", len(chain))
    }

    if "cause" != chain[0] {
        t.Fatalf("unexpected chain[0]: %s", chain[0])
    }
}

func TestBuildCauseContextChain_HugeMaxDepthDoesNotPanic(t *testing.T) {
    causeErr := NewError("cause", map[string]any{"key": "value"}, nil)

    chain := BuildCauseContextChain(causeErr, math.MaxInt)

    if 1 != len(chain) {
        t.Fatalf("expected 1 element for single-link chain, got: %d", len(chain))
    }

    if "value" != chain[0]["key"] {
        t.Fatalf("unexpected context entry: %v", chain[0])
    }
}
