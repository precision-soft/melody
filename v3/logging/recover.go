package logging

import (
    "fmt"
    "os"
    "runtime/debug"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* LogOnRecover recovers the panic in flight, logs it, and panics again when panicAgain is set. It never terminates the process: installed with defer it sits above the container teardown, the scope closes and the shutdown hooks, which an exit taken here would skip. A recovered *exception.ExitError is therefore re-panicked unchanged so its code reaches the owner of the process boundary — LogOnRecoverAndExit is the helper that takes the exit. */
func LogOnRecover(
    logger loggingcontract.Logger,
    panicAgain bool,
) {
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    exitError, isExitError := recoveredValue.(*exception.ExitError)
    if true == isExitError && nil != exitError {
        err := exitError.ErrorValue()
        if nil == err {
            err = exception.NewError(
                "exit requested with no error value",
                map[string]any{
                    "panicStack": string(debug.Stack()),
                },
                nil,
            )
        }

        if false == err.AlreadyLogged() {
            LogError(logger, err)
            err.MarkAsLogged()
        }

        if true == panicAgain {
            /* the wrapper is re-panicked rather than the error it carries: the exit code lives on the wrapper */
            exception.Exit(exitError)
        }

        return
    }

    if true == isAlreadyLoggedValue(recoveredValue) {
        if true == panicAgain {
            /* the recovered value is re-panicked unchanged rather than rebuilt: the mark that suppressed this record lives on it, and a fresh wrapper would carry none */
            panic(recoveredValue)
        }

        return
    }

    var err *exception.Error

    switch value := recoveredValue.(type) {
    case *exception.Error:
        if nil == value {
            err = newRecoveredPanicError(value)
        } else {
            err = value
        }

    case *exception.ExitError:
        /* only the typed nil reaches this case: the non-nil wrapper returned above */
        err = newRecoveredPanicError(value)

    case error:
        if true == internal.IsNilInterface(value) {
            err = newRecoveredPanicError(value)
        } else {
            err = exception.NewError(
                value.Error(),
                map[string]any{
                    "panicStack": string(debug.Stack()),
                },
                value,
            )
        }

    default:
        err = newRecoveredPanicError(value)
    }

    LogError(logger, err)
    err.MarkAsLogged()

    if true == panicAgain {
        exception.Panic(err)
    }
}

/* newRecoveredPanicError wraps a panic payload that carries no usable error together with the stack of the panic still in flight: the deferred handler runs with the panicking frames intact, and this is the only moment the origin of a runtime panic can be captured. */
func newRecoveredPanicError(value any) *exception.Error {
    return exception.NewError(
        "panic",
        map[string]any{
            "value":      value,
            "panicStack": string(debug.Stack()),
        },
        nil,
    )
}

func LogOnRecoverAndExit(
    logger loggingcontract.Logger,
    recovered any,
    exitCode int,
) {
    LogOnRecoverAndExitAfter(logger, recovered, exitCode, nil)
}

/* LogOnRecoverAndExitAfter logs the recovered value like LogOnRecoverAndExit and runs beforeExit between the record and the process exit. It is the one place that is both after the record and before the exit: a teardown deferred below never runs, because os.Exit skips it, and one run before closes the logger the final record must travel through. */
func LogOnRecoverAndExitAfter(
    logger loggingcontract.Logger,
    recovered any,
    exitCode int,
    beforeExit func(),
) {
    /* the rule NewExitError enforces, applied to the code this handler would exit with: zero makes the echo silent and os.Exit report success after a fatal failure. The refusal runs before the no-panic return so a caller wired with a bad code is caught on its first healthy pass, deterministically, not on the first panic months later. */
    if 1 > exitCode || 255 < exitCode {
        exception.Panic(
            exception.NewEmergency(
                "exit code out of range",
                map[string]any{
                    "exitCode": exitCode,
                },
                nil,
            ),
        )
    }

    if nil == recovered {
        return
    }

    err, resolvedExitCode, needsLogging := resolveRecoveredExitShielded(recovered, exitCode)

    /* every step between the recovery and the exit runs under its own recover: this is the last handler of the process, so a second panic must cost only its own step, never the resolved exit code, the stderr echo or os.Exit itself */
    if true == needsLogging {
        runExitStepShielded("logging the exit record", func() {
            LogError(logger, err)
            err.MarkAsLogged()
        })
    }

    /* the certificate is the destination twin of the stderr echo below, and the one record no operator threshold can drop: the detailed record above is written at the error's own level, which a threshold silently discards — the writer still marks it as logged, so the suppression is invisible even to this handler — and a process whose log file says nothing about its own death is what this line closes. It is written always, because it says something the detailed record does not: that the process is exiting, and with what code. */
    runExitStepShielded("logging the exit certificate", func() {
        writeExitCertificate(logger, err, resolvedExitCode)
    })

    if nil != beforeExit {
        runExitStepShielded("running the before-exit hook", func() {
            beforeExit()
        })
    }

    /* the earlier record may have gone to a file logger, leaving a container whose logs are the standard streams with no trace of a fatal exit */
    echoExitToStderr(err, resolvedExitCode)

    os.Exit(resolvedExitCode)
}

/* exitStepBudget is how long one step of the exit handler may run before it is abandoned; tests replace it to drive the timeout without real waits. Ten seconds is double the default http shutdown wait on purpose — the exit handler is the last resort, not the first — and it is a package constant rather than a tunable because this package cannot read the configuration: the logger it builds is what the configuration is loaded through. */
var exitStepBudget = 10 * time.Second

/* RunShieldedStep is the exit handler's own shield, offered to the one other caller that stands between a process and its end: the normal return of Run, whose teardown is deferred with no budget at all, so the healthy shutdown was the one without an emergency exit while the panicking one had a ten-second escape. It contains a panic inside the step, echoes it to stderr best-effort, and abandons a step that does not return within the budget, answering whether the step finished. A caller that gets false has a process holding something it cannot release and should end rather than wait.

The step keeps running on its goroutine after abandonment, so anything it writes must not be read by a caller that was told it did not finish. */
func RunShieldedStep(stepName string, step func()) bool {
    return runExitStepShielded(stepName, step)
}

/* runExitStepShielded contains a panic inside one step of the exit handler and echoes it to stderr best-effort, and abandons a step that does not return within the budget: the steps stand between a fatal failure and os.Exit, so a teardown blocked on a close that never returns — a drain on an unbuffered channel, a lock somebody died holding — would otherwise turn a dying process into a hung one, with the record written and the exit never taken. The step keeps running on its goroutine after abandonment; os.Exit ends it with the process. It answers whether the step finished inside its budget. */
func runExitStepShielded(stepName string, step func()) bool {
    stepDone := make(chan struct{})

    go func() {
        defer close(stepDone)

        /* the recover lives on the step's own goroutine: a recover in the waiting parent could never catch a panic raised here */
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                return
            }

            _, _ = fmt.Fprintf(os.Stderr, "melody: panic while %s during the exit handler: %v\n", stepName, recoveredValue)
        }()

        step()
    }()

    select {
    case <-stepDone:
        return true

    case <-time.After(exitStepBudget):
        _, _ = fmt.Fprintf(os.Stderr, "melody: %s did not return within %s during the exit handler; abandoning it\n", stepName, exitStepBudget)

        return false
    }
}

/* resolveRecoveredExitShielded contains a recovered value whose own methods panic — an Error() dereferencing the very nil field that made it panic-worthy, an Unwrap misbehaving under the already-logged probe. The resolve was the one step of this handler that ran outside the per-step shields, against the claim of the comment beside them, and its panic unwound into main: the process died with the Go runtime's exit code 2 — no record, no certificate, no stderr echo, no before-exit teardown. A resolve that panics is answered with a generic record under the caller's own exit code, which is exactly what the caller wired for a failure nothing can identify. */
func resolveRecoveredExitShielded(recovered any, exitCode int) (err *exception.Error, resolvedExitCode int, needsLogging bool) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        err = exception.NewEmergency(
            "recovered panic value could not be resolved",
            map[string]any{
                "resolvePanic": fmt.Sprintf("%v", recoveredValue),
            },
            nil,
        )
        resolvedExitCode = exitCode
        needsLogging = true
    }()

    return resolveRecoveredExit(recovered, exitCode)
}

/* resolveRecoveredExit normalizes a recovered value into the error the exit reports, the exit code the process takes, and whether that error still needs logging. An ExitError carries its own code; one holding no error value is given an error naming the anomaly instead of dereferencing nil inside the one handler that must not panic. A typed-nil exception is the value someone panicked with and is normalized as a plain panic value under the caller's code. */
func resolveRecoveredExit(
    recovered any,
    exitCode int,
) (*exception.Error, int, bool) {
    exitError, isExitError := recovered.(*exception.ExitError)
    if true == isExitError && nil != exitError {
        ownExitCode := exitError.ExitCode()

        /* the rule NewExitError enforces at construction, read again at the one door that decides how the process ends: the zero value is constructible outside the constructor and answers 0, which os.Exit would report as success after a fatal panic. A wrapper carrying an out-of-range code is not honored as an exit — it is normalized under the caller's code, like the typed nil below. The upper bound is latent by construction, since the fields are unexported and the constructor refuses anything outside the range. */
        if 1 > ownExitCode || 255 < ownExitCode {
            var cause error
            if carried := exitError.ErrorValue(); nil != carried {
                cause = carried
            }

            err := exception.NewError(
                "exit error carries an out-of-range exit code",
                map[string]any{
                    "exitCode": ownExitCode,
                },
                cause,
            )

            return err, exitCode, true
        }

        err := exitError.ErrorValue()

        if nil == err {
            /* latent defense: the zero value was the one producer of a nil error value and the range guard above intercepts it, but the branch keeps this reader answering instead of dereferencing nil should another producer ever appear */
            err = exception.NewError(
                "exit requested with no error value",
                nil,
                nil,
            )

            return err, ownExitCode, true
        }

        if true == err.AlreadyLogged() {
            return err, ownExitCode, false
        }

        return err, ownExitCode, true
    }

    alreadyLogged := isAlreadyLoggedValue(recovered)

    var err *exception.Error

    switch value := recovered.(type) {
    case *exception.Error:
        if nil == value {
            err = newRecoveredPanicError(value)
        } else {
            err = value
        }

    case *exception.ExitError:
        /* only the typed nil reaches this case: the non-nil wrapper returned above */
        err = newRecoveredPanicError(value)

    case error:
        if true == internal.IsNilInterface(value) {
            err = newRecoveredPanicError(value)
        } else {
            err = exception.NewError(
                value.Error(),
                map[string]any{
                    "panicStack": string(debug.Stack()),
                },
                value,
            )
        }

    default:
        err = newRecoveredPanicError(value)
    }

    return err, exitCode, false == alreadyLogged
}

/* writeExitCertificate writes the record that says the process is exiting and why, at emergency level, so it passes every threshold a deployment configures — the level exists for exactly this record: the system is about to be unusable. The error travels in the context rather than as the record's own subject, because the record's subject is the exit. */
func writeExitCertificate(logger loggingcontract.Logger, err *exception.Error, exitCode int) {
    if nil == logger || true == internal.IsNilInterface(logger) {
        return
    }

    logger.Emergency(
        "process exiting after unrecovered error",
        exception.LogContext(
            err,
            map[string]any{
                "exitCode": exitCode,
            },
        ),
    )
}

/* isAlreadyLoggedValue answers whether a recovered panic payload already carries the logged mark. A payload that is not an error carries none; everything else goes to exception.IsAlreadyLogged, so the recover helpers read the mark at the depth MarkLogged writes it. */
func isAlreadyLoggedValue(recovered any) bool {
    err, isError := recovered.(error)
    if false == isError {
        return false
    }

    return exception.IsAlreadyLogged(err)
}

/* echoExitToStderr writes one final line before a fatal exit so a non-zero exit is never completely silent on the standard streams, whatever destination the configured logger has. */
func echoExitToStderr(err error, exitCode int) {
    if 0 == exitCode {
        return
    }

    message := "-"
    if nil != err {
        message = err.Error()
    }

    _, _ = fmt.Fprintf(os.Stderr, "melody: exiting with code %d after unrecovered error: %s\n", exitCode, message)
}
