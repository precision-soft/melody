package exception

import (
    "errors"
    "fmt"
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

/* @info unclamped huge maxDepth must not drive the up-front allocation */

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

func TestBuildCauseContextChain_NilError_ReturnsNil(t *testing.T) {
    chain := BuildCauseContextChain(nil, 8)
    if nil != chain {
        t.Fatalf("expected nil chain for nil error")
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

func TestBuildCauseContextChain_ErrorWithNoContext_ReturnsNil(t *testing.T) {
    causeErr := errors.New("plain error")

    chain := BuildCauseContextChain(causeErr, 8)
    if nil != chain {
        t.Fatalf("expected nil chain when no exception has context")
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

/* @info a typed-nil error passes every nil comparison while any method call on it dereferences the nil receiver; the utilities must treat it as the nil it means instead of dying on the line that logs it */

func TestLogContext_TypedNilError_ReturnsNil(t *testing.T) {
    typedNil := (*Error)(nil)

    if nil != LogContext(typedNil) {
        t.Fatalf("expected nil context for a typed-nil error")
    }
}

func TestLogContext_TypedNilErrorWithExtra_ReturnsMergedExtras(t *testing.T) {
    typedNil := (*HttpException)(nil)

    context := LogContext(
        typedNil,
        map[string]any{"first": 1},
        map[string]any{"second": 2},
    )

    if 1 != context["first"] || 2 != context["second"] {
        t.Fatalf("expected both extras merged, got %v", context)
    }
}

/* nilProviderWrapper unwraps to a typed-nil provider, the shape errors.As matches and then hands over as a nil receiver */
type nilProviderWrapper struct {
}

func (instance *nilProviderWrapper) Error() string {
    return "wrapper"
}

func (instance *nilProviderWrapper) Unwrap() error {
    return (*HttpException)(nil)
}

func TestLogContext_TypedNilProviderInChain_DoesNotPanic(t *testing.T) {
    context := LogContext(&nilProviderWrapper{})

    if "wrapper" != context["error"] {
        t.Fatalf("expected the error key, got %v", context)
    }

    if _, hasCause := context["cause"]; true == hasCause {
        t.Fatalf("expected no cause for a typed-nil link, got %v", context)
    }
}

/* @info the variadic extras were silently truncated to the first map */
func TestLogContext_MergesEveryExtraInOrder(t *testing.T) {
    context := LogContext(
        errors.New("boom"),
        map[string]any{"first": 1, "shared": "first"},
        map[string]any{"second": 2, "shared": "second"},
    )

    if 1 != context["first"] || 2 != context["second"] {
        t.Fatalf("expected both extras merged, got %v", context)
    }

    if "second" != context["shared"] {
        t.Fatalf("expected the later extra to win, got %v", context["shared"])
    }
}

/* @info the cause walk is anchored on the top error's own wrap link: anchored on the nearest deep *Error it skipped every link above that error and dropped its own context from the record whenever it was not the top */
func TestLogContext_HttpExceptionWrappingError_KeepsTheInnerErrorAndItsContext(t *testing.T) {
    rootErr := errors.New("root")
    innerErr := NewError(
        "inner",
        map[string]any{"userId": 5},
        rootErr,
    )
    topErr := NewHttpExceptionWithCause(500, "boom", innerErr)

    context := LogContext(topErr)

    if "inner" != context["cause"] {
        t.Fatalf("expected the wrapped error as the cause, got %v", context["cause"])
    }

    causeChain, ok := context["causeChain"].([]string)
    if false == ok || 2 != len(causeChain) || "inner" != causeChain[0] || "root" != causeChain[1] {
        t.Fatalf("expected the chain to start at the wrapped error, got %v", context["causeChain"])
    }

    causeContextChain, ok := context["causeContextChain"].([]map[string]any)
    if false == ok || 2 != len(causeContextChain) {
        t.Fatalf("expected one chain entry per link, got %v", context["causeContextChain"])
    }

    if 5 != causeContextChain[0]["userId"] {
        t.Fatalf("expected the wrapped error's own context in the chain, got %v", causeContextChain[0])
    }
}

func TestFromError_TypedNil_ReturnsNil(t *testing.T) {
    typedNilError := (*Error)(nil)

    if nil != FromError(typedNilError) {
        t.Fatalf("expected nil for a typed-nil *Error")
    }

    /* the http exception variant is the one that used to panic: the assertion to *Error fails, so the walk reached err.Error() through the nil receiver */
    typedNilHttpException := (*HttpException)(nil)

    if nil != FromError(typedNilHttpException) {
        t.Fatalf("expected nil for a typed-nil *HttpException")
    }
}

func TestFromError_TypedNilProviderInChain_DoesNotPanic(t *testing.T) {
    ex := FromError(&nilProviderWrapper{})

    if nil == ex {
        t.Fatalf("expected a wrapped error")
    }

    if "wrapper" != ex.Message() {
        t.Fatalf("unexpected message %q", ex.Message())
    }
}

func TestFromErrorWithLevel_TypedNil_ReturnsNil(t *testing.T) {
    typedNil := (*HttpException)(nil)

    if nil != FromErrorWithLevel(typedNil, loggingcontract.LevelWarning) {
        t.Fatalf("expected nil for a typed-nil error")
    }
}

func TestFromErrorWithLevelAndContext_TypedNil_ReturnsNil(t *testing.T) {
    typedNil := (*Error)(nil)

    if nil != FromErrorWithLevelAndContext(typedNil, loggingcontract.LevelWarning, nil) {
        t.Fatalf("expected nil for a typed-nil error")
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

/* @info the mark is written at the depth the reader searches: logging.LogError finds the nearest *Error through errors.As, so a mark that stopped at a plain wrapper was a silent no-op and the one failure produced two records */
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

func TestBuildCauseChain_TypedNil_ReturnsNil(t *testing.T) {
    typedNil := (*Error)(nil)

    if nil != BuildCauseChain(typedNil, 8) {
        t.Fatalf("expected nil for a typed-nil error")
    }
}

func TestBuildCauseChain_TypedNilLink_EndsTheChain(t *testing.T) {
    topErr := NewError("top", nil, (*Error)(nil))

    chain := BuildCauseChain(topErr, 8)

    if 1 != len(chain) || "top" != chain[0] {
        t.Fatalf("expected the chain to end at the typed-nil link, got %v", chain)
    }
}

func TestBuildCauseContextChain_TypedNil_ReturnsNil(t *testing.T) {
    typedNil := (*HttpException)(nil)

    if nil != BuildCauseContextChain(typedNil, 8) {
        t.Fatalf("expected nil for a typed-nil error")
    }
}

func TestBuildCauseContextChain_TypedNilLink_EndsTheChain(t *testing.T) {
    withContext := NewError(
        "top",
        map[string]any{"key": "value"},
        (*Error)(nil),
    )

    chain := BuildCauseContextChain(withContext, 8)

    if 1 != len(chain) || "value" != chain[0]["key"] {
        t.Fatalf("expected one entry ending at the typed-nil link, got %v", chain)
    }
}

/* @info the immediate-node assertion is on ContextProvider, so an HttpException in the chain contributes its context instead of a silent nil */
func TestBuildCauseContextChain_HttpExceptionLink_ContributesItsContext(t *testing.T) {
    httpException := NewHttpException(404, "missing")
    httpException.SetContextValue("resource", "order")

    chain := BuildCauseContextChain(httpException, 8)

    if 1 != len(chain) || "order" != chain[0]["resource"] {
        t.Fatalf("expected the http exception context in the chain, got %v", chain)
    }
}

/* @info the entry guard is the one that stops the chain walk itself: errors.As calls Unwrap on a non-matching node, and a typed-nil node whose Unwrap dereferences the receiver panics inside the walk before the post-search guard can matter */
func TestMarkLogged_TypedNilWithUnwrap_DoesNotWalkTheChain(t *testing.T) {
    typedNil := (*ExitError)(nil)

    result := MarkLogged(typedNil)

    resultExit, ok := result.(*ExitError)
    if false == ok || nil != resultExit {
        t.Fatalf("expected the typed nil back, got %v", result)
    }
}
