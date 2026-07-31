package exception

import (
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

func NewExitError(exitCode int, err *Error) *ExitError {
    if nil == err {
        Panic(
            NewEmergency("exit error called with nil error", nil, nil),
        )
    }

    /* os.Exit hands the code to the operating system, which keeps its low 8 bits: 256 reports success from a dying process and a negative reads as 255 — neither is a code the caller can have meant — while 0 contradicts the error this constructor requires */
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
    /* the zero value is constructible outside the constructor that refuses a nil error; naming the anomaly beats dereferencing it at the exact moment the process boundary prints why it is exiting */
    if nil == instance.err {
        return "exit error carries no error value"
    }

    return instance.err.Error()
}

func (instance *ExitError) Unwrap() error {
    /* returning the nil field through the interface would box a typed nil that passes every nil comparison downstream */
    if nil == instance.err {
        return nil
    }

    return instance.err
}

func (instance *ExitError) ExitCode() int {
    return instance.exitCode
}

func (instance *ExitError) ErrorValue() *Error {
    return instance.err
}
