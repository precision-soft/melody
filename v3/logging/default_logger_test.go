package logging

import (
    "bytes"
    "log"
    "strings"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* captureStandardLog redirects the standard log package — the writer this logger prints through — for the duration of the callback and returns everything it wrote. The default logger holds no writer of its own, so the process-wide destination is the only place its records can be read back from. */
func captureStandardLog(callback func()) string {
    buffer := &bytes.Buffer{}

    previousWriter := log.Writer()
    previousFlags := log.Flags()

    log.SetOutput(buffer)
    log.SetFlags(0)

    defer func() {
        log.SetOutput(previousWriter)
        log.SetFlags(previousFlags)
    }()

    callback()

    return buffer.String()
}

func TestDefaultLogger_WritesTheLevelLabelAndTheMessage(t *testing.T) {
    logger := NewDefaultLogger()

    written := captureStandardLog(func() {
        logger.Log(loggingcontract.LevelWarning, "the message", loggingcontract.Context{"key": "value"})
    })

    if false == strings.Contains(written, "[warning] the message") {
        t.Fatalf("unexpected record: %q", written)
    }

    if false == strings.Contains(written, "{key=value}") {
        t.Fatalf("expected the context to be rendered, got %q", written)
    }
}

func TestDefaultLogger_EachLevelMethodFilesUnderItsOwnLevel(t *testing.T) {
    logger := NewDefaultLogger()

    methodList := []struct {
        name          string
        method        func(string, loggingcontract.Context)
        expectedLabel string
    }{
        {"Debug", logger.Debug, "debug"},
        {"Info", logger.Info, "info"},
        {"Warning", logger.Warning, "warning"},
        {"Error", logger.Error, "error"},
        {"Emergency", logger.Emergency, "emergency"},
    }

    for _, methodEntry := range methodList {
        written := captureStandardLog(func() {
            methodEntry.method("the message", nil)
        })

        if false == strings.HasPrefix(written, "["+methodEntry.expectedLabel+"] ") {
            t.Fatalf("%s: expected the record to open with [%s], got %q", methodEntry.name, methodEntry.expectedLabel, written)
        }
    }
}

func TestDefaultLogger_UsesTheConfiguredLabels(t *testing.T) {
    logger := NewDefaultLoggerWithLabels(loggingcontract.LevelLabels{
        loggingcontract.LevelError: loggingcontract.LevelLabelFromInt(3),
    })

    written := captureStandardLog(func() {
        logger.Error("the message", nil)
    })

    if false == strings.HasPrefix(written, "[3] the message") {
        t.Fatalf("unexpected record: %q", written)
    }
}

func TestDefaultLogger_RendersTheContextWithSortedKeys(t *testing.T) {
    logger := NewDefaultLogger()

    context := loggingcontract.Context{
        "zulu":  1,
        "alpha": 2,
        "mike":  3,
    }

    for iteration := 0; iteration < 20; iteration++ {
        written := captureStandardLog(func() {
            logger.Info("the message", context)
        })

        if false == strings.Contains(written, "{alpha=2 mike=3 zulu=1}") {
            t.Fatalf("expected the keys sorted and joined by one space, got %q", written)
        }
    }
}

func TestDefaultLogger_EmptyAndNilContext_RenderNothing(t *testing.T) {
    logger := NewDefaultLogger()

    for _, context := range []loggingcontract.Context{nil, {}} {
        written := captureStandardLog(func() {
            logger.Info("the message", context)
        })

        if true == strings.Contains(written, "{") {
            t.Fatalf("expected no braces for an empty context, got %q", written)
        }
    }
}

func TestDefaultLogger_SinglePairContext_CarriesNoSeparator(t *testing.T) {
    logger := NewDefaultLogger()

    written := captureStandardLog(func() {
        logger.Info("the message", loggingcontract.Context{"only": "pair"})
    })

    if false == strings.Contains(written, "{only=pair}") {
        t.Fatalf("expected a single pair with no separator, got %q", written)
    }
}

func TestNewDefaultLoggerWithLabels_CopiesTheLabelsTheCallerKeeps(t *testing.T) {
    labels := loggingcontract.LevelLabels{
        loggingcontract.LevelError: loggingcontract.LevelLabelFromString("error"),
    }

    logger := NewDefaultLoggerWithLabels(labels)

    /* the caller mutates the map it still holds, exactly what the copy exists to survive */
    labels[loggingcontract.LevelError] = loggingcontract.LevelLabelFromString("MUTATED")

    written := captureStandardLog(func() {
        logger.Error("a record", nil)
    })

    if false == strings.Contains(written, "[error]") {
        t.Fatalf("expected the logger to keep the labels it was built with, got %q", written)
    }

    if true == strings.Contains(written, "MUTATED") {
        t.Fatalf("expected the caller's later write not to reach the logger, got %q", written)
    }
}

/* one record stays one line: an unescaped line break would end the record and start a fully-formed fake one at whatever level the payload names, and a line-oriented shipper would ingest it as genuine. */
func TestDefaultLogger_KeepsOneRecordOneLine(t *testing.T) {
    logger := NewDefaultLogger()

    written := captureStandardLog(func() {
        logger.Info(
            "login failed for user bob\n[EMERGENCY] authentication subsystem disabled",
            loggingcontract.Context{"input": "a\rb"},
        )
    })

    if 1 != strings.Count(written, "\n") {
        t.Fatalf("expected exactly one line, got %q", written)
    }
    if true == strings.Contains(written, "\n[EMERGENCY]") {
        t.Fatalf("expected the forged record to stay inside the real one, got %q", written)
    }
    if false == strings.Contains(written, `bob\n[EMERGENCY]`) || false == strings.Contains(written, `a\rb`) {
        t.Fatalf("expected the escaped spellings, got %q", written)
    }
}
