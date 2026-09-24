package exception

import (
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

func NewExitError(exitCode int, err *Error) *ExitError {
    if nil == err {
        Panic(
            NewEmergency("exit error called with nil error", nil, nil),
        )
    }

    /* the operating system keeps only the low 8 bits, and 0 would contradict the error */
    if 1 > exitCode || 255 < exitCode {
        Panic(
            NewEmergency(
                "exit code out of range",
                exceptioncontract.Context{
                    "exitCode": exitCode,
                },
                nil,
            ),
        )
    }

    return &ExitError{
        exitCode: exitCode,
        err:      err,
    }
}

type ExitError struct {
    exitCode int
    err      *Error
}

func (instance *ExitError) Error() string {
    if nil == instance || nil == instance.err {
        return "exit error carries no error value"
    }

    return instance.err.Error()
}

func (instance *ExitError) Unwrap() error {
    /* a nil receiver answers nil, not a typed nil boxed through the interface */
    if nil == instance || nil == instance.err {
        return nil
    }

    return instance.err
}

/* ExitCode answers 0 on a nil receiver, the typed nil errors.As can match. */
func (instance *ExitError) ExitCode() int {
    if nil == instance {
        return 0
    }

    return instance.exitCode
}

func (instance *ExitError) ErrorValue() *Error {
    if nil == instance {
        return nil
    }

    return instance.err
}
