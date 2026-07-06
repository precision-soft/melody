package logging

import (
    "io"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func TestLogOnRecover_DoesNothingWhenNoPanic(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)
    }()

    if 0 != logger.calls {
        t.Fatalf("expected no log calls")
    }
}

func TestLogOnRecover_LogsExceptionError(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        exception.Panic(exception.NewError("boom", map[string]any{"a": "b"}, nil))
    }()

    if 1 != logger.calls {
        t.Fatalf("expected one log call")
    }

    if loggingcontract.LevelError != logger.lastLevel {
        t.Fatalf("unexpected level")
    }

    if "boom" != logger.lastMessage {
        t.Fatalf("unexpected message")
    }

    if "b" != logger.lastContext["a"] {
        t.Fatalf("unexpected context")
    }
}

func TestLogOnRecover_SkipsAlreadyLoggedException(t *testing.T) {
    logger := &captureLogger{}

    func() {
        defer LogOnRecover(logger, false)

        err := exception.NewError("boom", nil, nil)
        err.MarkAsLogged()

        exception.Panic(err)
    }()

    if 0 != logger.calls {
        t.Fatalf("expected no log calls")
    }
}

func TestLogOnRecover_PanicAgainRePanicsAndMarksLogged(t *testing.T) {
    logger := &captureLogger{}

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }

        err, ok := recoveredValue.(*exception.Error)
        if false == ok {
            t.Fatalf("expected *exception.Error")
        }

        if false == err.AlreadyLogged() {
            t.Fatalf("expected error to be marked as logged")
        }
    }()

    func() {
        defer LogOnRecover(logger, true)

        exception.Panic(exception.NewError("boom", nil, nil))
    }()
}

func TestEchoExitToStderr_WritesOneLineForNonZeroExit(t *testing.T) {
    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
    }()

    echoExitToStderr(exception.NewError("http server error", nil, nil), 1)

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    /* @important a fatal non-zero exit must leave a visible trace on stderr even when the configured logger writes elsewhere (e.g. a file logger): the pre-fix behavior exited completely silently on the AlreadyLogged path */
    if false == strings.Contains(string(output), "melody: exiting with code 1") || false == strings.Contains(string(output), "http server error") {
        t.Fatalf("expected the exit echo line on stderr, got %q", string(output))
    }
}

func TestEchoExitToStderr_StaysSilentForZeroExit(t *testing.T) {
    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
    }()

    echoExitToStderr(exception.NewError("clean exit", nil, nil), 0)

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if "" != string(output) {
        t.Fatalf("expected no stderr echo for a zero exit code, got %q", string(output))
    }
}
