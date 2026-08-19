package exception

import (
    "errors"
    "fmt"
    "math"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

func TestMarkLogged_TypedNil_ReturnsItUnchangedWithoutPanicking(t *testing.T) {
    typedNil := (*Error)(nil)

    result := MarkLogged(typedNil)

    resultError, ok := result.(*Error)
    if false == ok || nil != resultError {
        t.Fatalf("expected the typed nil back, got %v", result)
    }
}

func TestMarkLogged_MarksThroughAPlainWrapper(t *testing.T) {
    innerErr := NewError("inner", nil, nil)
    wrappedErr := fmt.Errorf("outer: %w", innerErr)

    result := MarkLogged(wrappedErr)

    if wrappedErr != result {
        t.Fatalf("expected the same error back")
    }

    if false == innerErr.AlreadyLogged() {
        t.Fatalf("expected the wrapped error to carry the mark")
    }
}

func TestMarkLogged_TypedNilWithUnwrap_DoesNotWalkTheChain(t *testing.T) {
    typedNil := (*ExitError)(nil)

    result := MarkLogged(typedNil)

    resultExit, ok := result.(*ExitError)
    if false == ok || nil != resultExit {
        t.Fatalf("expected the typed nil back, got %v", result)
    }
}

func TestIsAlreadyLogged_ReadsTheMarkAtTheDepthMarkLoggedWritesIt(t *testing.T) {
    httpException := NewHttpException(500, "reported once")

    if true == IsAlreadyLogged(httpException) {
        t.Fatalf("expected a fresh http exception to carry no mark")
    }

    _ = MarkLogged(httpException)

    if false == IsAlreadyLogged(httpException) {
        t.Fatalf("expected the mark written on the http exception to be read back")
    }
}

func TestIsAlreadyLogged_FindsTheMarkThroughAWrapper(t *testing.T) {
    marked := NewError("the reported failure", nil, nil)

    _ = MarkLogged(marked)

    wrapper := fmt.Errorf("while serving the request: %w", marked)

    if false == IsAlreadyLogged(wrapper) {
        t.Fatalf("expected the mark to be found through the wrapper")
    }
}

func TestIsAlreadyLogged_TypedNilCarriesNoMarkAndDoesNotPanic(t *testing.T) {
    var typedNil *Error

    var asError error = typedNil

    if nil == asError {
        t.Fatalf("test setup broken: the typed nil must be a non-nil interface")
    }

    if true == IsAlreadyLogged(asError) {
        t.Fatalf("expected a typed nil to carry no mark")
    }

    if true == IsAlreadyLogged(nil) {
        t.Fatalf("expected a nil error to carry no mark")
    }

    /* an *Error is matched by errors.As at the top node, so the post-search guard answers it and the entry guard is never the only thing standing. An *ExitError implements no AlreadyLogged, so the search has to walk it through Unwrap, which reads a field off the nil receiver. */
    var typedNilExit *ExitError

    var exitAsError error = typedNilExit

    if true == IsAlreadyLogged(exitAsError) {
        t.Fatalf("expected a typed nil exit error to carry no mark")
    }
}

func TestLogged_AForeignErrorComesBackAsAMarkedCarrier(t *testing.T) {
    original := errors.New("plain handler failure")

    reported := Logged(original)

    if true == IsAlreadyLogged(original) {
        t.Fatalf("a plain error has nowhere to carry the mark; it must not appear marked")
    }

    if false == IsAlreadyLogged(reported) {
        t.Fatalf("expected the carrier to report itself already logged")
    }

    if false == errors.Is(reported, original) {
        t.Fatalf("expected the carrier to keep the original as its cause")
    }
}

func TestLogged_AMelodyErrorIsMarkedInPlaceAndKeepsItsIdentity(t *testing.T) {
    original := NewError("melody failure", nil, nil)

    reported := Logged(original)

    if original != reported {
        t.Fatalf("expected the very same error back, not a wrapper")
    }

    if false == IsAlreadyLogged(original) {
        t.Fatalf("expected the original to carry the mark")
    }
}

func TestLogged_AnHttpExceptionKeepsItsStatusThroughTheCarrier(t *testing.T) {
    original := NotFound("no such article")

    reported := Logged(original)

    httpException := AsHttpException(reported)
    if nil == httpException {
        t.Fatalf("expected the status to stay resolvable")
    }

    if 404 != httpException.StatusCode() {
        t.Fatalf("expected 404, got %d", httpException.StatusCode())
    }
}

/* the wrapped shape is what the early return is for: on a bare melody error FromError is the identity, so only a foreign wrapper around a marked error can tell a return from a rewrap — and that is the shape a second writer meets, every writer between it and the failure having added its own context */
func TestLogged_AnAlreadyLoggedErrorIsNotWrappedAgain(t *testing.T) {
    original := MarkLogged(NewError("melody failure", nil, nil))

    if original != Logged(original) {
        t.Fatalf("expected an already marked error to come back untouched")
    }

    wrapped := fmt.Errorf("while saving the article: %w", original)

    if wrapped != Logged(wrapped) {
        t.Fatalf("expected a wrapper around a marked error to come back untouched, not wrapped a second time")
    }
}

func TestLogged_ANilErrorStaysNil(t *testing.T) {
    if nil != Logged(nil) {
        t.Fatalf("expected nil")
    }
}
