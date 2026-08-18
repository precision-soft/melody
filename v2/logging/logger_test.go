package logging

import (
    "bytes"
    "errors"
    "fmt"
    "io"
    "log"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/exception"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
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

func TestLogError_TypedNilError_DoesNothing(t *testing.T) {
    capture := &captureLogger{}

    LogError(capture, (*exception.Error)(nil))
    LogError(capture, (*typedNilProbeError)(nil))

    if 0 != capture.calls {
        t.Fatalf("expected no record for a typed-nil error, got %d", capture.calls)
    }
}

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

func TestEnrichContextWithCause_TypedNilCause_AddsNoCause(t *testing.T) {
    exceptionValue := exception.NewError("msg", nil, (*exception.Error)(nil))

    enrichedContext := enrichContextWithCause(exceptionValue)

    _, hasCause := enrichedContext["cause"]
    if true == hasCause {
        t.Fatalf("expected no cause for a typed-nil cause, got %v", enrichedContext["cause"])
    }
}

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

/* the capturing double stands in for the logger this file mirrors, so it lives here; the recover and request-logger tests reach it from the package. */
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

var _ loggingcontract.Logger = (*captureLogger)(nil)

func TestLogError_KeepsCallerSetCauseKeysOnAnException(t *testing.T) {
    capture := &captureLogger{}

    causeErr := exception.NewError(
        "driver: connection refused",
        map[string]any{
            "driverKey": "driverValue",
        },
        nil,
    )
    err := exception.NewError(
        "query failed",
        map[string]any{
            "cause":             "caller cause",
            "causeChain":        []string{"caller chain"},
            "causeContextChain": "caller context chain",
        },
        causeErr,
    )

    LogError(capture, err)

    if 1 != capture.calls {
        t.Fatalf("expected one record, got %d", capture.calls)
    }

    if "caller cause" != capture.lastContext["cause"] {
        t.Fatalf("expected the caller's cause value to be kept, got %v", capture.lastContext["cause"])
    }

    chain, isStringSlice := capture.lastContext["causeChain"].([]string)
    if false == isStringSlice || 1 != len(chain) || "caller chain" != chain[0] {
        t.Fatalf("expected the caller's cause chain to be kept, got %v", capture.lastContext["causeChain"])
    }

    if "caller context chain" != capture.lastContext["causeContextChain"] {
        t.Fatalf("expected the caller's cause context chain to be kept, got %v", capture.lastContext["causeContextChain"])
    }
}

func TestLogError_ComputesTheMissingCauseKeyBesideACallerSetOne(t *testing.T) {
    capture := &captureLogger{}

    causeErr := errors.New("driver: connection refused")
    err := exception.NewError(
        "query failed",
        map[string]any{
            "cause": "caller cause",
        },
        causeErr,
    )

    LogError(capture, err)

    if "caller cause" != capture.lastContext["cause"] {
        t.Fatalf("expected the caller's cause value to be kept, got %v", capture.lastContext["cause"])
    }

    chain, isStringSlice := capture.lastContext["causeChain"].([]string)
    if false == isStringSlice || 1 != len(chain) || "driver: connection refused" != chain[0] {
        t.Fatalf("expected the computed cause chain beside the kept cause, got %v", capture.lastContext["causeChain"])
    }
}

type foreignCauseKeyError struct {
    message string
    context loggingcontract.Context
    wrapped error
}

func (instance *foreignCauseKeyError) Error() string { return instance.message }

func (instance *foreignCauseKeyError) Context() loggingcontract.Context { return instance.context }

func (instance *foreignCauseKeyError) Unwrap() error { return instance.wrapped }

func TestLogError_KeepsCallerSetCauseKeysOnAForeignError(t *testing.T) {
    capture := &captureLogger{}

    err := &foreignCauseKeyError{
        message: "outer failure",
        context: loggingcontract.Context{
            "cause":             "caller cause",
            "causeContextChain": "caller context chain",
        },
        wrapped: exception.NewError(
            "inner failure",
            map[string]any{
                "innerKey": "innerValue",
            },
            nil,
        ),
    }

    LogError(capture, err)

    if 1 != capture.calls {
        t.Fatalf("expected one record, got %d", capture.calls)
    }

    if "caller cause" != capture.lastContext["cause"] {
        t.Fatalf("expected the caller's cause value to be kept, got %v", capture.lastContext["cause"])
    }

    chain, isStringSlice := capture.lastContext["causeChain"].([]string)
    if false == isStringSlice || 1 != len(chain) || "inner failure" != chain[0] {
        t.Fatalf("expected the computed cause chain beside the kept cause, got %v", capture.lastContext["causeChain"])
    }

    if "caller context chain" != capture.lastContext["causeContextChain"] {
        t.Fatalf("expected the caller's cause context chain to be kept, got %v", capture.lastContext["causeContextChain"])
    }
}

/* LevelEnabled is the single door onto the capability, and its answer for a logger that does not carry it is TRUE: the fallback is the reporting floor, so a site that spelled it the other way would stop recording against most loggers there are. Every caller asks through here rather than writing its own assertion, which is what keeps the rule in one place. */
func TestLevelEnabled_AnswersTrueForALoggerThatCannotBeAsked(t *testing.T) {
    if false == LevelEnabled(&captureLogger{}, loggingcontract.LevelDebug) {
        t.Fatalf("expected a logger without the capability to be reported enabled")
    }

    if false == LevelEnabled(NewJsonLogger(io.Discard, loggingcontract.LevelDebug), loggingcontract.LevelDebug) {
        t.Fatalf("expected a debug-level json logger to report debug enabled")
    }

    if true == LevelEnabled(NewJsonLogger(io.Discard, loggingcontract.LevelError), loggingcontract.LevelDebug) {
        t.Fatalf("expected an error-level json logger to report debug disabled")
    }

    if true == LevelEnabled(NewNopLogger(), loggingcontract.LevelEmergency) {
        t.Fatalf("expected the no-op logger to report nothing enabled")
    }
}
