package logging

import (
    "bytes"
    "log"
    "strings"
    "testing"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

/* @info this is the logger a process gets before any configuration is loaded — the one that carries the records of a boot that fails early — and no test had ever entered it. The record's shape is its whole contract: the label of the level in brackets, the message, then the context */
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

/* @info the five level methods are the whole public surface of the logger, and each one is a single delegation that a copy-paste can point at the wrong level: a Warning that files at debug disappears under any threshold above it */
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

/* @info the labels are configurable, and the logger must read them rather than the level's own name: a deployment that files by syslog number gets the number in every record or in none */
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

/* @info the context is rendered with its keys sorted, so two records carrying the same context read the same on every run: map iteration order is randomized per process, and an unsorted rendering makes the records of one failure impossible to compare or to grep for */
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

/* @info an empty context and a nil one render the same nothing: the braces exist to carry keys, and printing an empty pair of them on every context-less record is noise in the one logger that writes a boot failure */
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

/* @info the pair joiner puts exactly one space between pairs and none before the first: a leading separator changes every record's context, and the single-pair case is the one where an unconditional separator hides */
func TestDefaultLogger_SinglePairContext_CarriesNoSeparator(t *testing.T) {
    logger := NewDefaultLogger()

    written := captureStandardLog(func() {
        logger.Info("the message", loggingcontract.Context{"only": "pair"})
    })

    if false == strings.Contains(written, "{only=pair}") {
        t.Fatalf("expected a single pair with no separator, got %q", written)
    }
}
