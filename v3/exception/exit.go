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

    if nil == instance.err {
        return "exit error carries no error value"
    }

    return instance.err.Error()
}

func (instance *ExitError) Unwrap() error {

    if nil == instance.err {
        return nil
    }

    return instance.err
}

/* ExitCode answers the nil receiver with the out-of-range 0 rather than dereferencing it: errors.As matches this type on a typed-nil link and reports success with a nil pointer, which the callers that decide how the process ends read the code straight off. */
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
