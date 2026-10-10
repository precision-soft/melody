package logging

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type standardLoggerCapture struct {
    loggingcontract.Logger

    warnings []loggingcontract.Context
    errors   []loggingcontract.Context
}

func (instance *standardLoggerCapture) Warning(message string, context loggingcontract.Context) {
    instance.warnings = append(instance.warnings, context)
}

func (instance *standardLoggerCapture) Error(message string, context loggingcontract.Context) {
    instance.errors = append(instance.errors, context)
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

/* a handler panic net/http recovered outside the kernel is a defect that closed the connection, so it is filed at error, where a journal with an error threshold shows it; the connection-level lines beside it stay warnings */
func TestStandardErrorLogger_AHandlerPanicNetHttpRecoveredIsAnError(t *testing.T) {
    capture := &standardLoggerCapture{Logger: NewNopLogger()}
    standardLogger := NewStandardErrorLogger(capture, "http server error")

    standardLogger.Printf("http: panic serving 10.0.0.7:51234: the decorator failed\ngoroutine 42 [running]:\nmain.handler()")

    if 1 != len(capture.errors) || 0 != len(capture.warnings) {
        t.Fatalf("expected the panic line filed once at error, got %d errors and %d warnings", len(capture.errors), len(capture.warnings))
    }

    standardLogger.Printf("http: TLS handshake error from 10.0.0.7:51234: EOF")

    if 1 != len(capture.warnings) || 1 != len(capture.errors) {
        t.Fatalf("expected the handshake line filed at warning, got %d errors and %d warnings", len(capture.errors), len(capture.warnings))
    }
}
