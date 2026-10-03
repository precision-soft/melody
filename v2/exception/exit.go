package exception

import (
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
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
    /* the zero value is constructible outside the constructor that refuses a nil error, and a typed-nil receiver is the link errors.As matches, so Error answers for it */
    if nil == instance || nil == instance.err {
        return "exit error carries no error value"
    }

    return instance.err.Error()
}

func (instance *ExitError) Unwrap() error {
    /* errors.Is and errors.As call Unwrap on every link, so a typed-nil receiver answers nil rather than boxing the nil field into a typed nil that passes every nil comparison */
    if nil == instance || nil == instance.err {
        return nil
    }

    return instance.err
}

/* ExitCode answers the code in 1..255 the constructor admitted, or 0, a code it never admits, on a nil receiver: errors.As can match a typed-nil link, so a caller reads 0 as no exit code, never as a successful exit. */
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
