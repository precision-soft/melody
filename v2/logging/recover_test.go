package logging

import (
    "testing"

    "github.com/precision-soft/melody/v2/exception"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
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
