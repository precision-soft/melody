package logging

import (
    "bytes"
    "errors"
    "io"
    "log"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

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

func TestEnrichContextWithCause_AddsCauseChainFromBuildCauseChain(t *testing.T) {
    innerErr := exception.NewError("inner cause", map[string]any{"ik": "iv"}, nil)
    outerErr := exception.NewError("outer", nil, innerErr)

    context := enrichContextWithCause(outerErr)

    causeValue, hasCause := context["cause"]
    if false == hasCause {
        t.Fatalf("expected cause field")
    }

    causeString, ok := causeValue.(string)
    if false == ok {
        t.Fatalf("expected cause to be string, got %T", causeValue)
    }

    if "inner cause" != causeString {
        t.Fatalf("unexpected cause: %s", causeString)
    }

    causeChainValue, hasCauseChain := context["causeChain"]
    if false == hasCauseChain {
        t.Fatalf("expected causeChain field")
    }

    causeChainSlice, ok := causeChainValue.([]string)
    if false == ok {
        t.Fatalf("expected causeChain to be []string, got %T", causeChainValue)
    }

    if 1 != len(causeChainSlice) {
        t.Fatalf("expected 1 element in cause chain, got %d", len(causeChainSlice))
    }

    causeContextChainValue, hasCauseContextChain := context["causeContextChain"]
    if false == hasCauseContextChain {
        t.Fatalf("expected causeContextChain field")
    }

    causeContextChainSlice, ok := causeContextChainValue.([]map[string]any)
    if false == ok {
        t.Fatalf("expected causeContextChain to be []map[string]any, got %T", causeContextChainValue)
    }

    if 1 != len(causeContextChainSlice) {
        t.Fatalf("expected 1 element in causeContextChain, got %d", len(causeContextChainSlice))
    }

    if "iv" != causeContextChainSlice[0]["ik"] {
        t.Fatalf("unexpected causeContextChain content: %v", causeContextChainSlice[0])
    }
}

func TestEnrichContextWithCause_NoCause_ReturnsContextOnly(t *testing.T) {
    err := exception.NewError("message", map[string]any{"key": "value"}, nil)

    context := enrichContextWithCause(err)

    if "value" != context["key"] {
        t.Fatalf("expected original context preserved")
    }

    _, hasCause := context["cause"]
    if true == hasCause {
        t.Fatalf("expected no cause field when no cause")
    }
}

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
