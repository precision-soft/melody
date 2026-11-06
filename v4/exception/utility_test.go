package exception

import (
    "errors"
    "fmt"
    "math"
    "strings"
    "testing"
    "time"

    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

func TestFromErrorAs_TheZeroConfigAnswersWhatFromErrorAnswers(t *testing.T) {
    if nil != FromErrorAs(nil, FromErrorConfig{}) {
        t.Fatalf("expected nil for nil")
    }

    own := NewWarning("own", nil, nil)
    if own != FromErrorAs(own, FromErrorConfig{}) {
        t.Fatalf("expected an *Error passed through unchanged")
    }

    plainErr := errors.New("plain")
    wrapped := FromErrorAs(plainErr, FromErrorConfig{})
    if loggingcontract.LevelError != wrapped.Level() || "plain" != wrapped.Message() || false == errors.Is(wrapped, plainErr) {
        t.Fatalf("expected the plain error wrapped at error level, got %v %q", wrapped.Level(), wrapped.Message())
    }
}

func TestFromErrorAs_AContextAloneWrapsAtErrorLevel(t *testing.T) {
    own := NewWarning("own", nil, nil)

    wrapped := FromErrorAs(own, FromErrorConfig{Context: exceptioncontract.Context{"orderId": 7}})
    if own == wrapped {
        t.Fatalf("expected a config that sets something to wrap even an *Error")
    }

    if loggingcontract.LevelError != wrapped.Level() || 7 != wrapped.Context()["orderId"] {
        t.Fatalf("expected the context at error level, got %v %v", wrapped.Level(), wrapped.Context())
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
    context := LogContext(nil, LogContextConfig{})
    if nil != context {
        t.Fatalf("expected nil context for nil error")
    }
}

func TestLogContext_NilErrorWithExtra_ReturnsMerged(t *testing.T) {
    extra := map[string]any{"key": "value"}
    context := LogContext(nil, LogContextConfig{Extra: extra})
    if nil == context {
        t.Fatalf("expected non-nil context")
    }

    if "value" != context["key"] {
        t.Fatalf("unexpected context value")
    }
}

func TestLogContext_PlainError_SetsErrorField(t *testing.T) {
    plainErr := errors.New("plain error")

    context := LogContext(plainErr, LogContextConfig{})
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

    context := LogContext(exceptionErr, LogContextConfig{})
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
    exceptionErr := NewError("msg", map[string]any{"existing": "yes", "shared": "error"}, nil)
    extra := map[string]any{"extra": "value", "shared": "extra"}

    context := LogContext(exceptionErr, LogContextConfig{Extra: extra})
    if nil == context {
        t.Fatalf("expected non-nil context")
    }

    if "yes" != context["existing"] {
        t.Fatalf("expected existing context to be preserved")
    }

    if "value" != context["extra"] {
        t.Fatalf("expected extra context to be merged")
    }

    if "extra" != context["shared"] {
        t.Fatalf("expected the extra's key to win over the error's, got %v", context["shared"])
    }
}

func TestLogContext_TypedNilError_ReturnsNil(t *testing.T) {
    typedNil := (*Error)(nil)

    if nil != LogContext(typedNil, LogContextConfig{}) {
        t.Fatalf("expected nil context for a typed-nil error")
    }
}

func TestLogContext_TypedNilErrorWithExtra_ReturnsACopyOfTheExtra(t *testing.T) {
    typedNil := (*HttpException)(nil)
    extra := map[string]any{"first": 1, "second": 2}

    context := LogContext(typedNil, LogContextConfig{Extra: extra})

    if 2 != len(context) || 1 != context["first"] || 2 != context["second"] {
        t.Fatalf("expected the extra answered, got %v", context)
    }

    context["third"] = 3

    if _, leaked := extra["third"]; true == leaked {
        t.Fatalf("expected a copy, the caller's extra was written through")
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
    context := LogContext(&nilProviderWrapper{}, LogContextConfig{})

    if "wrapper" != context["error"] {
        t.Fatalf("expected the error key, got %v", context)
    }

    if _, hasCause := context["cause"]; true == hasCause {
        t.Fatalf("expected no cause for a typed-nil link, got %v", context)
    }
}

func TestLogContext_CauseChainDepth_IsEightWhenLeftOutAndCutsBothChains(t *testing.T) {
    var causeErr error
    for index := 12; 0 < index; index-- {
        causeErr = NewError(fmt.Sprintf("link %d", index), exceptioncontract.Context{"link": index}, causeErr)
    }
    topErr := fmt.Errorf("top: %w", causeErr)

    for depth, expectedLinks := range map[int]int{0: 8, 1: 1, 2: 2, 10: 10, 20: 12} {
        context := LogContext(topErr, LogContextConfig{CauseChainDepth: depth})

        causeChain, _ := context["causeChain"].([]string)
        causeContextChain, _ := context["causeContextChain"].([]map[string]any)
        if expectedLinks != len(causeChain) || expectedLinks != len(causeContextChain) {
            t.Fatalf("depth %d: expected %d links on both chains, got %d and %d", depth, expectedLinks, len(causeChain), len(causeContextChain))
        }

        if "link 1" != context["cause"] {
            t.Fatalf("depth %d: expected the first link as the cause, got %v", depth, context["cause"])
        }
    }
}

func TestLogContext_ANegativeCauseChainDepth_PanicsNamingTheFieldBeforeTheErrorIsRead(t *testing.T) {
    for name, err := range map[string]error{
        "an error":    errors.New("boom"),
        "a nil error": nil,
        "a typed nil": (*Error)(nil),
    } {
        func() {
            defer func() {
                refusal, isError := recover().(*Error)
                if false == isError {
                    t.Fatalf("%s: expected an *Error panic", name)
                }

                if "the limit is below its minimum" != refusal.Message() {
                    t.Fatalf("%s: unexpected message %q", name, refusal.Message())
                }

                if "LogContextConfig.CauseChainDepth" != refusal.Context()["field"] || "-1" != refusal.Context()["value"] || "1" != refusal.Context()["minimum"] {
                    t.Fatalf("%s: expected the field, the value and the minimum named, got %v", name, refusal.Context())
                }
            }()

            LogContext(err, LogContextConfig{Extra: exceptioncontract.Context{"key": "value"}, CauseChainDepth: -1})
        }()
    }
}

func TestLogContext_ACallersMergedExtra_KeepsItsOrder(t *testing.T) {
    merged := map[string]any{}
    for _, extra := range []map[string]any{
        {"first": 1, "shared": "first"},
        {"second": 2, "shared": "second"},
    } {
        for key, value := range extra {
            merged[key] = value
        }
    }

    context := LogContext(errors.New("boom"), LogContextConfig{Extra: merged})

    if 1 != context["first"] || 2 != context["second"] {
        t.Fatalf("expected both extras merged, got %v", context)
    }

    if "second" != context["shared"] {
        t.Fatalf("expected the later extra to win, got %v", context["shared"])
    }
}

func TestLogContext_HttpExceptionWrappingError_KeepsTheInnerErrorAndItsContext(t *testing.T) {
    rootErr := errors.New("root")
    innerErr := NewError(
        "inner",
        map[string]any{"userId": 5},
        rootErr,
    )
    topErr := NewHttpError(500, HttpErrorConfig{Message: "boom", Cause: innerErr})

    context := LogContext(topErr, LogContextConfig{})

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

func TestFromErrorAs_ALevel_TypedNil_ReturnsNil(t *testing.T) {
    typedNil := (*HttpException)(nil)

    if nil != FromErrorAs(typedNil, FromErrorConfig{Level: loggingcontract.LevelWarning}) {
        t.Fatalf("expected nil for a typed-nil error")
    }
}

func TestFromErrorAs_ALevelAndAContext_TypedNil_ReturnsNil(t *testing.T) {
    typedNil := (*Error)(nil)

    if nil != FromErrorAs(typedNil, FromErrorConfig{Level: loggingcontract.LevelWarning, Context: nil}) {
        t.Fatalf("expected nil for a typed-nil error")
    }
}

func TestIsNilInterfaceValue_PlainNil_AnswersTrue(t *testing.T) {
    if false == isNilInterfaceValue(nil) {
        t.Fatalf("expected a plain nil to read as nil")
    }
}

func TestLogContext_NilExtra_AddsNothingOnBothPaths(t *testing.T) {
    onlyNilExtra := LogContext(nil, LogContextConfig{})

    if nil != onlyNilExtra {
        t.Fatalf("expected nil when the extra is nil, got %v", onlyNilExtra)
    }

    emptyExtra := LogContext(nil, LogContextConfig{Extra: exceptioncontract.Context{}})

    if nil == emptyExtra || 0 != len(emptyExtra) {
        t.Fatalf("expected an empty copy of an empty extra, got %v", emptyExtra)
    }

    withError := LogContext(errors.New("boom"), LogContextConfig{})

    if 1 != len(withError) || "boom" != withError["error"] {
        t.Fatalf("expected the error's context alone beside a nil extra, got %v", withError)
    }
}

func TestLogContext_ProviderContextCannotOverwriteTheErrorKey(t *testing.T) {
    err := NewError(
        "the real message",
        map[string]any{
            "error":       "a value the producer put under the reserved key",
            "serviceName": "service.mailer",
        },
        nil,
    )

    context := LogContext(err, LogContextConfig{})

    if "the real message" != context["error"] {
        t.Fatalf("expected the error's own message under the error key, got %v", context["error"])
    }

    if "service.mailer" != context["serviceName"] {
        t.Fatalf("expected the rest of the provider context to survive, got %v", context)
    }
}

func TestBuildCauseContextChain_NonPositiveMaxDepth_WalksOneLink(t *testing.T) {
    inner := NewError("inner", map[string]any{"key": "value"}, nil)
    outer := NewError("outer", map[string]any{"outerKey": "outerValue"}, inner)

    for _, maxDepth := range []int{0, -1} {
        chain := BuildCauseContextChain(outer, maxDepth)

        if 1 != len(chain) {
            t.Fatalf("maxDepth %d: expected exactly one link, got %v", maxDepth, chain)
        }

        if "outerValue" != chain[0]["outerKey"] {
            t.Fatalf("maxDepth %d: expected the first link's own context, got %v", maxDepth, chain[0])
        }
    }
}

func TestBuildCauseContextChain_ProviderWithEmptyContext_KeepsItsPositionAsNil(t *testing.T) {
    inner := NewError("inner", map[string]any{"key": "value"}, nil)
    middle := NewError("middle", nil, inner)
    outer := NewError("outer", nil, middle)

    chain := BuildCauseContextChain(outer, 8)

    if 3 != len(chain) {
        t.Fatalf("expected one entry per link, got %v", chain)
    }

    if nil != chain[0] || nil != chain[1] {
        t.Fatalf("expected the context-less links to hold nil, got %v", chain)
    }

    if "value" != chain[2]["key"] {
        t.Fatalf("expected the innermost context at its own position, got %v", chain)
    }
}

/* valueError is an error whose concrete type is a struct value, not a pointer: it cannot be nil, and the typed-nil detection has to answer that from the kind alone instead of asking a value that has no IsNil */
type valueError struct {
    message string
}

func (instance valueError) Error() string {
    return instance.message
}

func TestErrorUtilities_ValueTypedError_IsNotReadAsNil(t *testing.T) {
    if true == isNilInterfaceValue(valueError{message: "boom"}) {
        t.Fatalf("expected a struct-valued error not to read as nil")
    }

    context := LogContext(valueError{message: "boom"}, LogContextConfig{})

    if "boom" != context["error"] {
        t.Fatalf("expected the value-typed error to be described, got %v", context)
    }

    converted := FromError(valueError{message: "boom"})

    if nil == converted || "boom" != converted.Message() {
        t.Fatalf("expected the value-typed error to convert, got %v", converted)
    }
}

func TestFromErrorAs_ALevel_CarriesTheRequestedLevelAndWrapsTheError(t *testing.T) {
    foreignError := errors.New("foreign")

    converted := FromErrorAs(foreignError, FromErrorConfig{Level: loggingcontract.LevelInfo})

    if nil == converted {
        t.Fatalf("expected a converted error")
    }

    if loggingcontract.LevelInfo != converted.Level() {
        t.Fatalf("expected level %q, got %q", loggingcontract.LevelInfo, converted.Level())
    }

    if "foreign" != converted.Message() {
        t.Fatalf("unexpected message %q", converted.Message())
    }

    if false == errors.Is(converted, foreignError) {
        t.Fatalf("expected the original error to stay reachable as the cause")
    }
}

func TestFromErrorAs_ALevel_TakesTheContextOfTheProviderInTheChain(t *testing.T) {
    inner := NewError("inner", map[string]any{"serviceName": "service.mailer"}, nil)

    converted := FromErrorAs(fmt.Errorf("wrapped: %w", inner), FromErrorConfig{Level: loggingcontract.LevelWarning})

    if nil == converted {
        t.Fatalf("expected a converted error")
    }

    if "service.mailer" != converted.Context()["serviceName"] {
        t.Fatalf("expected the provider's context, got %v", converted.Context())
    }

    if loggingcontract.LevelWarning != converted.Level() {
        t.Fatalf("expected level %q, got %q", loggingcontract.LevelWarning, converted.Level())
    }
}

func TestFromErrorAs_ALevelAndAContext_MergesTheProviderContextUnderTheGivenOne(t *testing.T) {
    inner := NewError(
        "inner",
        map[string]any{
            "serviceName": "service.mailer",
            "attempt":     1,
        },
        nil,
    )

    converted := FromErrorAs(
        fmt.Errorf("wrapped: %w", inner),
        FromErrorConfig{
            Level: loggingcontract.LevelEmergency,
            Context: map[string]any{
                "attempt": 2,
                "host":    "mail.example.com",
            },
        },
    )

    if nil == converted {
        t.Fatalf("expected a converted error")
    }

    if "service.mailer" != converted.Context()["serviceName"] {
        t.Fatalf("expected the provider key to survive, got %v", converted.Context())
    }

    if 2 != converted.Context()["attempt"] {
        t.Fatalf("expected the given context to win the shared key, got %v", converted.Context()["attempt"])
    }

    if "mail.example.com" != converted.Context()["host"] {
        t.Fatalf("expected the given context to be merged, got %v", converted.Context())
    }

    if loggingcontract.LevelEmergency != converted.Level() {
        t.Fatalf("expected level %q, got %q", loggingcontract.LevelEmergency, converted.Level())
    }
}

func TestFromErrorAs_ALevelAndAContext_WithoutProviderOrContext_StillConverts(t *testing.T) {
    converted := FromErrorAs(errors.New("plain"), FromErrorConfig{Level: loggingcontract.LevelDebug, Context: nil})

    if nil == converted {
        t.Fatalf("expected a converted error")
    }

    if 0 != len(converted.Context()) {
        t.Fatalf("expected an empty context, got %v", converted.Context())
    }

    if loggingcontract.LevelDebug != converted.Level() {
        t.Fatalf("expected level %q, got %q", loggingcontract.LevelDebug, converted.Level())
    }
}

func TestFromError_TakesTheContextOfTheProviderInTheChain(t *testing.T) {
    inner := NewError("inner", map[string]any{"serviceName": "service.mailer"}, nil)

    converted := FromError(fmt.Errorf("wrapped: %w", inner))

    if nil == converted {
        t.Fatalf("expected a converted error")
    }

    if "service.mailer" != converted.Context()["serviceName"] {
        t.Fatalf("expected the provider's context, got %v", converted.Context())
    }
}

func TestFromErrorAs_TypedNilProviderInChain_DoNotPanic(t *testing.T) {
    withLevel := FromErrorAs(&nilProviderWrapper{}, FromErrorConfig{Level: loggingcontract.LevelWarning})

    if nil == withLevel || "wrapper" != withLevel.Message() {
        t.Fatalf("expected the wrapper to convert, got %v", withLevel)
    }

    withContext := FromErrorAs(
        &nilProviderWrapper{},
        FromErrorConfig{
            Level:   loggingcontract.LevelWarning,
            Context: map[string]any{"key": "value"},
        },
    )

    if nil == withContext || "value" != withContext.Context()["key"] {
        t.Fatalf("expected the given context to survive, got %v", withContext)
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

func TestBuildCauseContextChain_HttpExceptionLink_ContributesItsContext(t *testing.T) {
    httpException := NewHttpException(404, "missing")
    httpException.SetContextValue("resource", "order")

    chain := BuildCauseContextChain(httpException, 8)

    if 1 != len(chain) || "order" != chain[0]["resource"] {
        t.Fatalf("expected the http exception context in the chain, got %v", chain)
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

/* LogContext runs inside recovery defers: a value whose Error() panics — often on the very nil field that made it panic-worthy — must cost its text, never the record or the connection. */
func TestLogContext_ContainsAPanickingErrorMessage(t *testing.T) {
    logContext := LogContext(&panickingTextError{}, LogContextConfig{})

    errorText, isString := logContext["error"].(string)
    if false == isString {
        t.Fatalf("expected the error text to render, got %T", logContext["error"])
    }

    if false == strings.Contains(errorText, "error message panicked") {
        t.Fatalf("expected the contained panic note, got %q", errorText)
    }
}

/* the cause chain renders every link's text under the same containment: a panicking cause buried in the chain took the whole record down from inside the recovery that was writing it. */
func TestBuildCauseChain_ContainsAPanickingLink(t *testing.T) {
    chain := BuildCauseChain(&panickingTextError{}, 8)

    if 1 != len(chain) {
        t.Fatalf("expected one rendered link, got %d", len(chain))
    }

    if false == strings.Contains(chain[0], "error message panicked") {
        t.Fatalf("expected the contained panic note, got %q", chain[0])
    }
}

type panickingTextError struct{}

func (instance *panickingTextError) Error() string {
    var m map[string]string
    m["boom"] = "boom"

    return "unreachable"
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

/* an error-shaped panic value belongs in the cause slot: kept only in a context slot it collapses to its bare message at the render boundary, so the context and the cause chain of the very error that was raised reach no record. */
func TestPanicCause_AnErrorShapedPanicTravelsAsTheCause(t *testing.T) {
    rootCause := errors.New("connection refused")
    panicValue := NewError("config parameter is not defined", nil, rootCause)

    cause := PanicCause(panicValue)
    if nil == cause {
        t.Fatal("expected the error-shaped panic value to answer as the cause")
    }

    if false == errors.Is(cause, rootCause) {
        t.Fatalf("expected the cause chain to survive, got %v", cause)
    }
}

/* a typed nil answers no cause: its Error() dereferences a nil receiver, so a cause slot holding it would detonate at the first render of the record the boundary exists to write. */
func TestPanicCause_ATypedNilAnswersNoCause(t *testing.T) {
    var typedNil *Error

    if nil != PanicCause(typedNil) {
        t.Fatal("expected a typed-nil panic value to answer no cause")
    }
}

/* a panic value that is not an error has no cause to give; the recovery boundary still renders it into the context. */
func TestPanicCause_ANonErrorPanicAnswersNoCause(t *testing.T) {
    if nil != PanicCause("scheduled command panicked on purpose") {
        t.Fatal("expected a non-error panic value to answer no cause")
    }

    if nil != PanicCause(nil) {
        t.Fatal("expected a nil panic value to answer no cause")
    }
}

/* TestLogContext_AJoinedCauseReachesTheRecord pins the readers against the second unwrap shape the standard library defines. Every walk here anchored on the single-valued errors.Unwrap, which answers nothing at all for an errors.Join: a failure that gathered what several replicas had to say arrived as one flattened line of text, with the context of every branch — the host that refused, the destination that timed out — reaching no record. The writers this framework repaired are one producer of the shape; a join can arrive from any dependency and from any application. */
func TestLogContext_AJoinedCauseReachesTheRecord(t *testing.T) {
    firstCause := NewError(
        "the primary replica refused the write",
        exceptioncontract.Context{"host": "db-1"},
        nil,
    )
    secondCause := NewError(
        "the standby replica refused the write",
        exceptioncontract.Context{"host": "db-2"},
        nil,
    )

    top := NewError(
        "could not persist the order",
        exceptioncontract.Context{"orderId": 42},
        errors.Join(firstCause, secondCause),
    )

    context := LogContext(top, LogContextConfig{})

    causeChain, isChain := context["causeChain"].([]string)
    if false == isChain {
        t.Fatalf("expected a cause chain, got %v", context["causeChain"])
    }

    if 3 != len(causeChain) {
        t.Fatalf("expected the join and both of its branches in the chain, got %v", causeChain)
    }

    if false == strings.Contains(causeChain[1], "primary replica") || false == strings.Contains(causeChain[2], "standby replica") {
        t.Fatalf("expected both branches named in the chain, got %v", causeChain)
    }

    causeContextChain, isContextChain := context["causeContextChain"].([]map[string]any)
    if false == isContextChain {
        t.Fatalf("expected a cause context chain, got %v", context["causeContextChain"])
    }

    if 3 != len(causeContextChain) {
        t.Fatalf("expected one context slot per link, got %v", causeContextChain)
    }

    if "db-1" != causeContextChain[1]["host"] || "db-2" != causeContextChain[2]["host"] {
        t.Fatalf("expected each branch to keep its own context, got %v", causeContextChain)
    }
}

/* a join handed straight to the reader, with nothing wrapping it, is the shape an application produces when it gathers failures and returns them as they are */
func TestLogContext_ABareJoinIsWalkedAsItsBranches(t *testing.T) {
    joined := errors.Join(
        NewError("the first destination refused", exceptioncontract.Context{"destination": "a"}, nil),
        NewError("the second destination refused", exceptioncontract.Context{"destination": "b"}, nil),
    )

    context := LogContext(joined, LogContextConfig{})

    causeChain, isChain := context["causeChain"].([]string)
    if false == isChain || 2 != len(causeChain) {
        t.Fatalf("expected both branches of a bare join, got %v", context["causeChain"])
    }

    causeContextChain, isContextChain := context["causeContextChain"].([]map[string]any)
    if false == isContextChain || 2 != len(causeContextChain) {
        t.Fatalf("expected a context slot per branch, got %v", context["causeContextChain"])
    }

    if "a" != causeContextChain[0]["destination"] || "b" != causeContextChain[1]["destination"] {
        t.Fatalf("expected each branch to keep its own context, got %v", causeContextChain)
    }
}

/* the depth budget counts LINKS, not levels: a join of many branches must not cost more than a deep chain of the same length */
func TestBuildCauseChain_TheDepthBudgetBoundsAWideJoin(t *testing.T) {
    branches := make([]error, 0, 12)
    for index := 0; index < 12; index++ {
        branches = append(branches, NewError("branch", nil, nil))
    }

    chain := BuildCauseChain(errors.Join(branches...), 4)

    if 4 != len(chain) {
        t.Fatalf("expected the budget to bound the walk, got %d entries", len(chain))
    }
}

/* a single-wrap chain keeps exactly the sequence it always produced: the walk changed shape, and nothing that was already right may move */
func TestBuildCauseChain_ASingleWrapChainIsUnchanged(t *testing.T) {
    deepest := NewError("deepest", nil, nil)
    middle := NewError("middle", nil, deepest)
    top := NewError("top", nil, middle)

    chain := BuildCauseChain(top, 8)

    if 3 != len(chain) || "top" != chain[0] || "middle" != chain[1] || "deepest" != chain[2] {
        t.Fatalf("expected the chain walked top down, got %v", chain)
    }
}

type contextPanicProviderError struct{}

func (instance *contextPanicProviderError) Error() string {
    return "the provider refused"
}

func (instance *contextPanicProviderError) Context() exceptioncontract.Context {
    panic("context rendering gave up")
}

/* renderErrorText already keeps a panicking Error() from unwinding through the recovery defer LogContext runs inside of, but a foreign Context() ran bare — at the top provider, in the cause-context walk, and in the From* constructors that run on the same recovery paths. The record survives with the panic value in the context's place instead of the process unwinding on a second panic. */
func TestLogContext_AProviderWhoseContextPanicsIsContained(t *testing.T) {
    foreignErr := &contextPanicProviderError{}

    logContext := LogContext(foreignErr, LogContextConfig{})
    if nil == logContext {
        t.Fatalf("expected the record to survive the panicking context")
    }

    if "context rendering gave up" != logContext["contextPanicked"] {
        t.Fatalf("expected the panic value to be kept in the context's place, got %#v", logContext)
    }

    wrapped := NewError("wrapper", nil, foreignErr)
    wrappedContext := LogContext(wrapped, LogContextConfig{})
    if nil == wrappedContext {
        t.Fatalf("expected the record of the wrapper to survive the cause's panicking context")
    }
}

func TestFromError_AProviderWhoseContextPanicsIsContained(t *testing.T) {
    foreignErr := &contextPanicProviderError{}

    converted := FromError(foreignErr)
    if nil == converted {
        t.Fatalf("expected the conversion to survive the panicking context")
    }

    if "context rendering gave up" != converted.Context()["contextPanicked"] {
        t.Fatalf("expected the panic value to be kept in the context's place, got %#v", converted.Context())
    }
}

type panickingMessageError struct{}

func (panickingMessageError) Error() string {
    panic("Error() panics")
}

/* FromError runs inside the kernel's recovery defers; an Error() that panics there raised a second panic through the defer that was reporting the first, so the message is rendered the way LogContext renders it */
func TestFromError_AnErrorWhoseMessagePanicsIsStillWrapped(t *testing.T) {
    wrapped := FromError(panickingMessageError{})

    if nil == wrapped || "error message panicked: Error() panics" != wrapped.Message() {
        t.Fatalf("expected the recovered message, got %v", wrapped)
    }

    if !errors.Is(wrapped, wrapped.Unwrap()) || nil == wrapped.Unwrap() {
        t.Fatalf("expected the panicking error kept as the cause")
    }
}

func TestFromErrorAs_ALevel_AnErrorWhoseMessagePanicsIsStillWrapped(t *testing.T) {
    wrapped := FromErrorAs(panickingMessageError{}, FromErrorConfig{Level: loggingcontract.LevelWarning})

    if nil == wrapped || "error message panicked: Error() panics" != wrapped.Message() {
        t.Fatalf("expected the recovered message, got %v", wrapped)
    }
}

func TestFromErrorAs_ALevelAndAContext_AnErrorWhoseMessagePanicsIsStillWrapped(t *testing.T) {
    wrapped := FromErrorAs(panickingMessageError{}, FromErrorConfig{Level: loggingcontract.LevelWarning, Context: exceptioncontract.Context{"key": "value"}})

    if nil == wrapped || "error message panicked: Error() panics" != wrapped.Message() || "value" != wrapped.Context()["key"] {
        t.Fatalf("expected the recovered message with the context, got %v", wrapped)
    }
}

/* Logged is what the kernel's recovery defers return; the mark on the result is what separates it from FromError alone */
func TestLogged_AnErrorWhoseMessagePanicsIsMarkedAndReturned(t *testing.T) {
    logged := Logged(panickingMessageError{})

    if nil == logged || false == IsAlreadyLogged(logged) {
        t.Fatalf("expected a marked error back, got %v", logged)
    }
}

func TestBuildCauseContextChain_StaysIndexAlignedWithBuildCauseChainAcrossAJoin(t *testing.T) {
    var typedNil *Error
    withContext := NewError("carries context", map[string]any{"key": "value"}, nil)
    plain := errors.New("carries none")
    top := NewError("top", map[string]any{"top": 1}, errors.Join(typedNil, withContext, plain))

    textChain := BuildCauseChain(top, 8)
    contextChain := BuildCauseContextChain(top, 8)

    if len(textChain) != len(contextChain) {
        t.Fatalf("expected the two chains to have one entry per link, got %d and %d", len(textChain), len(contextChain))
    }

    if 4 != len(textChain) {
        t.Fatalf("expected the typed nil to contribute nothing and the other three links to be walked, got %#v", textChain)
    }

    contextIndex := -1
    for index, text := range textChain {
        if "carries context" == text {
            contextIndex = index
        }
    }

    if -1 == contextIndex {
        t.Fatalf("the probe no longer walks the link that carries context: %#v", textChain)
    }

    if "value" != contextChain[contextIndex]["key"] {
        t.Fatalf("expected the context of link %d beside its text, got %#v", contextIndex, contextChain)
    }
}

type panickingUnwrapError struct{}

func (panickingUnwrapError) Error() string {
    return "a link whose Unwrap panics"
}

func (panickingUnwrapError) Unwrap() error {
    panic("Unwrap() panics")
}

type panickingJoinUnwrapError struct{}

func (panickingJoinUnwrapError) Error() string {
    return "a join whose Unwrap panics"
}

func (panickingJoinUnwrapError) Unwrap() []error {
    panic("Unwrap() []error panics")
}

type panickingAsError struct{}

func (panickingAsError) Error() string {
    return "a link whose As panics"
}

func (panickingAsError) As(any) bool {
    panic("As() panics")
}

type panickingMarkError struct{}

func (panickingMarkError) Error() string {
    return "a link whose mark panics"
}

func (panickingMarkError) AlreadyLogged() bool {
    panic("AlreadyLogged() panics")
}

func (panickingMarkError) MarkAsLogged() {
    panic("MarkAsLogged() panics")
}

/* LogContext runs in the recovery defers that file a failure — the async audit worker's among them, on a bare goroutine — and an Unwrap that panicked raised a second panic there, past the recover already spent, which ended the process. The walk now stops at that link with the reason written into the chain, and the message is kept */
func TestLogContext_AnErrorWhoseUnwrapPanicsKeepsItsMessageAndNamesTheCut(t *testing.T) {
    context := LogContext(panickingUnwrapError{}, LogContextConfig{})

    if "a link whose Unwrap panics" != context["error"] {
        t.Fatalf("expected the message kept, got %#v", context["error"])
    }

    if "the links below could not be read, their Unwrap panicked: Unwrap() panics" != context["cause"] {
        t.Fatalf("expected the cut named as the cause, got %#v", context["cause"])
    }
}

func TestLogContext_AJoinWhoseUnwrapPanicsKeepsItsMessageAndNamesTheCut(t *testing.T) {
    context := LogContext(panickingJoinUnwrapError{}, LogContextConfig{})

    if "a join whose Unwrap panics" != context["error"] {
        t.Fatalf("expected the message kept, got %#v", context["error"])
    }

    if "the links below could not be read, their Unwrap panicked: Unwrap() []error panics" != context["cause"] {
        t.Fatalf("expected the cut named as the cause, got %#v", context["cause"])
    }
}

/* the search for a context provider calls the As of every link, so an As that panics took LogContext down before the walk began */
func TestLogContext_AnErrorWhoseAsPanicsKeepsItsMessage(t *testing.T) {
    context := LogContext(panickingAsError{}, LogContextConfig{Extra: exceptioncontract.Context{"table": "audit"}})

    if "a link whose As panics" != context["error"] || "audit" != context["table"] {
        t.Fatalf("expected the message and the extra context kept, got %#v", context)
    }
}

/* the link that panics sits BELOW a wrap here, which is where a recovery boundary puts it: exception.NewError(..., PanicCause(recovered)) */
func TestLogContext_AWrapOverALinkWhoseUnwrapPanicsKeepsTheChainAboveTheCut(t *testing.T) {
    wrapped := NewError("audit storage panicked while saving the entry", nil, panickingUnwrapError{})

    context := LogContext(wrapped, LogContextConfig{})

    causeChain, isChain := context["causeChain"].([]string)
    if false == isChain || 2 != len(causeChain) {
        t.Fatalf("expected the link and the cut in the chain, got %#v", context["causeChain"])
    }

    if "a link whose Unwrap panics" != causeChain[0] || false == strings.HasPrefix(causeChain[1], "the links below could not be read") {
        t.Fatalf("expected the link then the cut, got %#v", causeChain)
    }
}

func TestBuildCauseChain_StopsAtALinkWhoseUnwrapPanics(t *testing.T) {
    chain := BuildCauseChain(panickingUnwrapError{}, 8)

    if 2 != len(chain) || "a link whose Unwrap panics" != chain[0] || false == strings.HasSuffix(chain[1], "Unwrap() panics") {
        t.Fatalf("expected the link then the cut, got %#v", chain)
    }
}

func TestFromError_AnErrorWhoseAsPanicsIsStillWrapped(t *testing.T) {
    for name, convert := range map[string]func(error) *Error{
        "FromError": FromError,
        "FromErrorAs with a level": func(err error) *Error {
            return FromErrorAs(err, FromErrorConfig{Level: loggingcontract.LevelWarning})
        },
        "FromErrorAs with a level and a context": func(err error) *Error {
            return FromErrorAs(err, FromErrorConfig{Level: loggingcontract.LevelWarning, Context: exceptioncontract.Context{"key": "value"}})
        },
    } {
        converted := convert(panickingAsError{})

        if nil == converted || "a link whose As panics" != converted.Message() {
            t.Fatalf("%s: expected the error wrapped under its message, got %v", name, converted)
        }
    }
}

/* a mark that cannot be left or read costs a second record of the failure, never the record itself */
func TestMarkLogged_AMarkThatPanicsLeavesTheErrorAsItWas(t *testing.T) {
    foreignErr := panickingMarkError{}

    if returned := MarkLogged(foreignErr); returned != error(foreignErr) {
        t.Fatalf("expected the error handed back unchanged, got %v", returned)
    }

    if true == IsAlreadyLogged(foreignErr) {
        t.Fatalf("expected a mark that panics to read as not logged")
    }
}

func TestIsAlreadyLogged_AChainWhoseSearchPanicsReadsAsNotLogged(t *testing.T) {
    if true == IsAlreadyLogged(panickingAsError{}) || true == IsAlreadyLogged(panickingUnwrapError{}) {
        t.Fatalf("expected a chain that cannot be searched to read as not logged")
    }
}

/* Logged wraps what it could not mark in a marked melody error, so the reader after it still finds the mark */
func TestLogged_AnErrorWhoseMarkPanicsIsWrappedMarked(t *testing.T) {
    logged := Logged(panickingMarkError{})

    if nil == logged || false == IsAlreadyLogged(logged) {
        t.Fatalf("expected a marked error back, got %v", logged)
    }
}

type contextDoublyPanickingValue struct{}

func (instance contextDoublyPanickingValue) Error() string {
    panic(panickingMessageError{})
}

type contextDoublyPanickingProviderError struct{}

func (instance *contextDoublyPanickingProviderError) Error() string {
    return "the provider refused"
}

func (instance *contextDoublyPanickingProviderError) Context() exceptioncontract.Context {
    panic(contextDoublyPanickingValue{})
}

/* the context's panic value is rendered inside the recover that caught it, so a value whose rendering panics twice is named by its type instead of raising past the record */
func TestLogContext_AContextPanicValueWhoseRenderingPanicsIsNamedByItsType(t *testing.T) {
    logContext := LogContext(&contextDoublyPanickingProviderError{}, LogContextConfig{})

    rendered, _ := logContext["contextPanicked"].(string)
    if false == strings.Contains(rendered, "contextDoublyPanickingValue") || false == strings.Contains(rendered, "rendering panicked") {
        t.Fatalf("expected the panic value named by its type, got %#v", logContext)
    }
}

type contextChainWrapper struct {
    inner error
}

func (instance *contextChainWrapper) Error() string {
    return "wrapper"
}

func (instance *contextChainWrapper) Unwrap() error {
    return instance.inner
}

type receiverContextError struct {
    context exceptioncontract.Context
    panics  bool
}

func (instance *receiverContextError) Error() string {
    return "receiver context error"
}

func (instance *receiverContextError) Context() exceptioncontract.Context {
    if true == instance.panics {
        panic("context exploded")
    }

    return instance.context
}

func TestContextDoors_ReadATypedNilProviderAsNoContextAndAPanickingOneAsPanicked(t *testing.T) {
    doors := map[string]func(err error) string{
        "LogContext": func(err error) string { return fmt.Sprint(LogContext(err, LogContextConfig{})) },
        "FromError":  func(err error) string { return fmt.Sprint(FromError(err).Context()) },
        "FromErrorAs with a level": func(err error) string {
            return fmt.Sprint(FromErrorAs(err, FromErrorConfig{Level: loggingcontract.LevelError}).Context())
        },
        "FromErrorAs with a level and a context": func(err error) string {
            return fmt.Sprint(FromErrorAs(err, FromErrorConfig{Level: loggingcontract.LevelError, Context: exceptioncontract.Context{"key": "value"}}).Context())
        },
        "BuildCauseContextChain": func(err error) string { return fmt.Sprint(BuildCauseContextChain(err, 8)) },
    }

    typedNil := &contextChainWrapper{inner: (*receiverContextError)(nil)}
    panicking := &contextChainWrapper{inner: &receiverContextError{panics: true}}

    for name, door := range doors {
        if rendered := door(typedNil); true == strings.Contains(rendered, "contextPanicked") {
            t.Fatalf("%s: expected a typed-nil provider read as no context, got %s", name, rendered)
        }

        if rendered := door(panicking); false == strings.Contains(rendered, "contextPanicked") {
            t.Fatalf("%s: expected a panicking provider read as panicked, got %s", name, rendered)
        }
    }
}

type cyclicChainLink struct {
    next error
}

func (instance *cyclicChainLink) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainLink) Unwrap() error {
    return instance.next
}

func newCyclicChain() *cyclicChainLink {
    first := &cyclicChainLink{}
    first.next = &cyclicChainLink{next: first}

    return first
}

func TestChainDoors_ReturnOnAChainThatReachesItself(t *testing.T) {
    done := make(chan exceptioncontract.Context, 1)
    go func() {
        chain := newCyclicChain()
        _ = IsAlreadyLogged(chain)
        _ = MarkLogged(chain)
        done <- LogContext(chain, LogContextConfig{})
    }()

    select {
    case context := <-done:
        chainEntries, isChain := context["causeChain"].([]string)
        if false == isChain || 2 != len(chainEntries) {
            t.Fatalf("expected the two links once each, got %#v", context["causeChain"])
        }
    case <-time.After(5 * time.Second):
        t.Fatal("the chain doors must return on a cyclic chain")
    }
}

func TestChainHolds_FindsWhatErrorsAsFindsDepthFirst(t *testing.T) {
    target := &cyclicChainLink{next: errors.New("end")}
    joined := errors.Join(errors.New("first"), fmt.Errorf("wrapped: %w", target))

    var found *cyclicChainLink
    if false == chainHolds(joined, &found) || target != found {
        t.Fatalf("expected the wrapped link through the join, got %v", found)
    }

    var absent *Error
    if true == chainHolds(joined, &absent) {
        t.Fatal("expected nothing of a type the chain does not hold")
    }

    if true == chainHolds(nil, &absent) {
        t.Fatal("expected a nil error to hold nothing")
    }
}

type selfUnwrappingValue struct{}

func (instance selfUnwrappingValue) Error() string {
    return "self unwrapping value"
}

func (instance selfUnwrappingValue) Unwrap() error {
    return instance
}

func TestChainHolds_BoundsAChainWithoutPointerIdentity(t *testing.T) {
    done := make(chan bool, 1)
    go func() {
        var absent *Error
        done <- chainHolds(selfUnwrappingValue{}, &absent)
    }()

    select {
    case found := <-done:
        if true == found {
            t.Fatal("expected nothing of a type the chain does not hold")
        }
    case <-time.After(5 * time.Second):
        t.Fatal("the search must end on a chain it cannot recognise a cycle in")
    }
}

type chainIsSelfReachingError struct{}

func (instance *chainIsSelfReachingError) Error() string {
    return "self reaching"
}

func (instance *chainIsSelfReachingError) Unwrap() error {
    return instance
}

type chainIsMatchingError struct {
    target error
}

func (instance *chainIsMatchingError) Error() string {
    return "matching"
}

func (instance *chainIsMatchingError) Is(target error) bool {
    return instance.target == target
}

type chainIsUncomparableError struct {
    links []string
}

func (instance chainIsUncomparableError) Error() string {
    return "uncomparable"
}

func TestChainIs_AnswersWhatErrorsIsAnswers(t *testing.T) {
    target := errors.New("target")

    for name, testCase := range map[string]struct {
        err      error
        expected bool
    }{
        "the target itself":    {err: target, expected: true},
        "wrapped":              {err: fmt.Errorf("wrap: %w", target), expected: true},
        "joined":               {err: errors.Join(errors.New("other"), target), expected: true},
        "through Is":           {err: &chainIsMatchingError{target: target}, expected: true},
        "another error":        {err: errors.New("target"), expected: false},
        "nil":                  {err: nil, expected: false},
        "a chain reaching it":  {err: &chainIsSelfReachingError{}, expected: false},
        "an uncomparable link": {err: chainIsUncomparableError{links: []string{"a"}}, expected: false},
    } {
        if actual := ChainIs(testCase.err, target); testCase.expected != actual {
            t.Fatalf("%s: expected %v, got %v", name, testCase.expected, actual)
        }
    }
}
