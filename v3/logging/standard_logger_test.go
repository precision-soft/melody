package logging

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type standardLoggerCapture struct {
    loggingcontract.Logger

    warnings []loggingcontract.Context
}

func (instance *standardLoggerCapture) Warning(message string, context loggingcontract.Context) {
    instance.warnings = append(instance.warnings, context)
}

func TestStandardErrorLogger_TrimsTheStandardLoggersNewline(t *testing.T) {
    capture := &standardLoggerCapture{Logger: NewNopLogger()}

    NewStandardErrorLogger(capture, "http server error").Printf("http: Accept error: too many open files")

    if 1 != len(capture.warnings) {
        t.Fatalf("expected one record, got %d", len(capture.warnings))
    }

    if "http: Accept error: too many open files" != capture.warnings[0]["line"] {
        t.Fatalf("expected the trailing newline gone, got %q", capture.warnings[0]["line"])
    }
}

func TestStandardErrorLogger_AnEmptyLineWritesNothing(t *testing.T) {
    capture := &standardLoggerCapture{Logger: NewNopLogger()}

    NewStandardErrorLogger(capture, "http server error").Printf("")

    if 0 != len(capture.warnings) {
        t.Fatalf("expected an empty line to write nothing, got %v", capture.warnings)
    }
}

func TestStandardErrorLogger_ANilLoggerIsInert(t *testing.T) {
    NewStandardErrorLogger(nil, "http server error").Printf("anything")
}
