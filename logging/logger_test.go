package logging

import (
    "bytes"
    "errors"
    "fmt"
    "log"
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

type typedNilProbeError struct {
    message string
}

func (instance *typedNilProbeError) Error() string {
    return instance.message
}

/* @info a typed nil boxed in the error interface is the nil its producer meant: the entry guard used to compare against the untyped nil alone, so the value walked on and dereferenced — Level() through the errors.As extraction for an exception, Error() on the generic path for anything else — inside the function whose whole job is to record a failure */
func TestLogError_TypedNilError_DoesNothing(t *testing.T) {
    capture := &captureLogger{}

    LogError(capture, (*exception.Error)(nil))
    LogError(capture, (*typedNilProbeError)(nil))

    if 0 != capture.calls {
        t.Fatalf("expected no record for a typed-nil error, got %d", capture.calls)
    }
}

/* @info a typed nil found mid-chain matched the errors.As extraction and handed a nil receiver to Level(); the record now anchors on the wrapper itself and the nil link contributes nothing */
func TestLogError_TypedNilExceptionInTheChain_LogsTheWrapper(t *testing.T) {
    wrapper := fmt.Errorf("wrapper failed: %w", error((*exception.Error)(nil)))

    capture := &captureLogger{}
    LogError(capture, wrapper)

    if 1 != capture.calls {
        t.Fatalf("expected one record for the wrapper, got %d", capture.calls)
    }

    if loggingcontract.LevelError != capture.lastLevel {
        t.Fatalf("expected the record at error level, got %s", capture.lastLevel)
    }

    if false == strings.Contains(capture.lastMessage, "wrapper failed") {
        t.Fatalf("expected the wrapper message, got %q", capture.lastMessage)
    }
}

/* @info a typed-nil logger passed the plain nil comparison and the first method call dereferenced the nil receiver; the fallback the untyped nil already took is the right answer for both */
func TestLogError_TypedNilLogger_FallsBackToTheProcessLogger(t *testing.T) {
    var buffer bytes.Buffer

    originalWriter := log.Writer()
    log.SetOutput(&buffer)
    defer func() {
        log.SetOutput(originalWriter)
    }()

    LogError((*jsonLogger)(nil), exception.NewError("fallback message", nil, nil))

    if false == strings.Contains(buffer.String(), "fallback message") {
        t.Fatalf("expected the record on the process logger, got: %s", buffer.String())
    }
}

/* @info the mark is read at the depth exception.MarkLogged writes it — the nearest AlreadyLogged implementer in the chain. The reader used to search for the nearest *exception.Error instead, so marking a wrapping http exception was invisible here and the one failure produced two records. */
func TestLogError_ReadsTheMarkAtTheDepthMarkLoggedWrites(t *testing.T) {
    inner := exception.NewError("inner", nil, nil)
    outer := exception.NewHttpExceptionWithCause(500, "outer", inner)

    _ = exception.MarkLogged(outer)

    capture := &captureLogger{}
    LogError(capture, outer)

    if 0 != capture.calls {
        t.Fatalf("expected the marked wrapper to suppress the record, got %d", capture.calls)
    }
}

/* @info the record anchors on the error the caller handed over: the http exception wrapping a low-severity exception used to be logged as that inner exception — its message, its info level — which dropped the wrapper's framing and filed the whole record below an info-filtering logger's threshold */
func TestLogError_AnchorsOnTheTopError(t *testing.T) {
    inner := exception.NewInfo("cache miss", nil, nil)
    outer := exception.NewHttpExceptionWithCause(500, "failed to answer the request", inner)

    capture := &captureLogger{}
    LogError(capture, outer)

    if 1 != capture.calls {
        t.Fatalf("expected one record, got %d", capture.calls)
    }

    if loggingcontract.LevelError != capture.lastLevel {
        t.Fatalf("expected the record at error level, got %s", capture.lastLevel)
    }

    if false == strings.Contains(capture.lastMessage, "failed to answer the request") {
        t.Fatalf("expected the wrapper message, got %q", capture.lastMessage)
    }

    causeValue, hasCause := capture.lastContext["cause"]
    if false == hasCause {
        t.Fatalf("expected the wrapped exception as the cause")
    }

    if "cache miss" != causeValue {
        t.Fatalf("unexpected cause: %v", causeValue)
    }
}

/* @info the already-logged check used to sit behind the nil-logger fallback, so an error already recorded through a real logger was printed a second time whenever no logger was at hand */
func TestLogError_AlreadyLogged_NilLogger_PrintsNothing(t *testing.T) {
    var buffer bytes.Buffer

    originalWriter := log.Writer()
    log.SetOutput(&buffer)
    defer func() {
        log.SetOutput(originalWriter)
    }()

    err := exception.NewError("already recorded", nil, nil)
    err.MarkAsLogged()

    LogError(nil, err)

    if 0 != buffer.Len() {
        t.Fatalf("expected no output for an already-logged error, got: %s", buffer.String())
    }
}

func TestLogError_ForeignErrorCarriesProviderContextAndCauseChain(t *testing.T) {
    root := errors.New("root cause")
    outer := exception.NewHttpExceptionWithCause(500, "outer", root)
    outer.SetContextValue("key", "value")

    capture := &captureLogger{}
    LogError(capture, outer)

    if 1 != capture.calls {
        t.Fatalf("expected one record, got %d", capture.calls)
    }

    if "value" != capture.lastContext["key"] {
        t.Fatalf("expected the provider context in the record, got %v", capture.lastContext)
    }

    if "root cause" != capture.lastContext["cause"] {
        t.Fatalf("expected the cause chain in the record, got %v", capture.lastContext)
    }
}

/* @info BuildCauseChain refuses a typed-nil cause at the entry and returns an empty chain, which routed the walk into the branch that renders the cause directly — the only input that ever reached it — where Error() dereferenced the nil receiver */
func TestEnrichContextWithCause_TypedNilCause_AddsNoCause(t *testing.T) {
    exceptionValue := exception.NewError("msg", nil, (*exception.Error)(nil))

    enrichedContext := enrichContextWithCause(exceptionValue)

    _, hasCause := enrichedContext["cause"]
    if true == hasCause {
        t.Fatalf("expected no cause for a typed-nil cause, got %v", enrichedContext["cause"])
    }
}

/* @info the nil-logger fallback is the path a record takes before the container exists — a boot failure — and the context is the whole diagnostic: the branch that prints it had never been entered, so nothing said the fallback carries more than the message */
func TestLogError_NilLogger_PrintsTheEnrichedContext(t *testing.T) {
    var buffer bytes.Buffer

    originalWriter := log.Writer()
    log.SetOutput(&buffer)
    defer func() {
        log.SetOutput(originalWriter)
    }()

    LogError(
        nil,
        exception.NewError(
            "boot failed",
            map[string]any{"serviceName": "service.logger"},
            errors.New("the cause"),
        ),
    )

    written := buffer.String()

    if false == strings.Contains(written, "boot failed") {
        t.Fatalf("expected the message, got %q", written)
    }

    if false == strings.Contains(written, "serviceName:service.logger") {
        t.Fatalf("expected the context, got %q", written)
    }

    if false == strings.Contains(written, "the cause") {
        t.Fatalf("expected the cause chain in the context, got %q", written)
    }
}

/* @info the same fallback for an error that is not an exception: it is filed at error level under its full message, with the context of the nearest provider in its chain — the branch that prints that context had never been entered either */
func TestLogError_NilLogger_ForeignErrorPrintsItsProviderContext(t *testing.T) {
    var buffer bytes.Buffer

    originalWriter := log.Writer()
    log.SetOutput(&buffer)
    defer func() {
        log.SetOutput(originalWriter)
    }()

    inner := exception.NewError("inner", map[string]any{"serviceName": "service.mailer"}, nil)

    LogError(nil, fmt.Errorf("wrapper failed: %w", inner))

    written := buffer.String()

    if false == strings.Contains(written, "[ERROR] wrapper failed") {
        t.Fatalf("expected the foreign error at error level under its full message, got %q", written)
    }

    if false == strings.Contains(written, "serviceName:service.mailer") {
        t.Fatalf("expected the provider context, got %q", written)
    }
}

/* @info a foreign error whose chain carries contexts contributes them as the cause context chain, exactly as an exception's own enrichment does; the two enrichments are written twice and can drift apart */
func TestLogError_ForeignErrorCarriesTheCauseContextChain(t *testing.T) {
    inner := exception.NewError("inner", map[string]any{"userId": 5}, nil)

    capture := &captureLogger{}

    LogError(capture, fmt.Errorf("wrapper failed: %w", inner))

    if 1 != capture.calls {
        t.Fatalf("expected one record, got %d", capture.calls)
    }

    causeContextChain, isChain := capture.lastContext["causeContextChain"].([]map[string]any)
    if false == isChain || 0 == len(causeContextChain) {
        t.Fatalf("expected the cause context chain, got %v", capture.lastContext["causeContextChain"])
    }

    if 5 != causeContextChain[0]["userId"] {
        t.Fatalf("expected the inner context in the chain, got %v", causeContextChain[0])
    }
}
