package logging

import (
    "context"
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
    /* there is no hook to budget, so the figure is the one this package would have used anyway */
    LogOnRecoverAndExitAfter(logger, recovered, exitCode, exitStepBudget, nil)
}

/* LogOnRecoverAndExitAfter logs the recovered value like LogOnRecoverAndExit and runs beforeExit between the record and the process exit. It is the one place that is both after the record and before the exit: a teardown deferred below never runs, because os.Exit skips it, and one run before closes the logger the final record must travel through.

   The hook runs under the budget its caller declares, not under this package's constant. The two are the same teardown reached by two doors — a process that returns from Run and one that panics or takes an exit error release the same brokers, pools and tracer providers — so a budget honoured on one door and ignored on the other would leave every cli command that exits non-zero closing under a figure its operator had already replaced. What this package cannot read is the CONFIGURATION; the value is not configuration by the time it arrives here, it is an argument. A non-positive budget carries the same meaning it carries everywhere else in this file: no deadline. */
func LogOnRecoverAndExitAfter(
    logger loggingcontract.Logger,
    recovered any,
    exitCode int,
    beforeExitBudget time.Duration,
    beforeExit func(beforeExitContext context.Context),
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
        runExitStepShielded("logging the exit record", func(_ context.Context) {
            LogError(logger, err)
            err.MarkAsLogged()
        })
    }

    /* the certificate is the destination twin of the stderr echo below, and the one record no operator threshold can drop: the detailed record above is written at the error's own level, which a threshold silently discards — the writer still marks it as logged, so the suppression is invisible even to this handler — and a process whose log file says nothing about its own death is what this line closes. It is written always, because it says something the detailed record does not: that the process is exiting, and with what code. */
    runExitStepShielded("logging the exit certificate", func(_ context.Context) {
        writeExitCertificate(logger, err, resolvedExitCode)
    })

    if nil != beforeExit {
        runExitStepShieldedWithin(beforeExitBudget, "running the before-exit hook", beforeExit)
    }

    /* the earlier record may have gone to a file logger, leaving a container whose logs are the standard streams with no trace of a fatal exit.

       It runs under the same budget the steps above run under, but not through their shield: the shield narrates an abandoned step ON STDERR, and stderr is exactly the channel this step can be blocked on — a pipe to a collector that stopped reading — so the report would park the process one budget later, on the line written to say the previous line was abandoned. Nothing is said here on the abandoned path; os.Exit is what the process was owed, and it is taken. */
    echoDone := make(chan struct{})

    go func() {
        defer close(echoDone)

        /* a panic here would replace the exit code this process was owed with the runtime's own; the echo is best-effort by construction and the record above is already written */
        defer func() {
            _ = recover()
        }()

        echoExitToStderr(err, resolvedExitCode)
    }()

    select {
    case <-echoDone:

    case <-time.After(exitStepBudget):
    }

    os.Exit(resolvedExitCode)
}

/* exitStepBudget is how long one step of the exit handler may run before it is abandoned; tests replace it to drive the timeout without real waits. Ten seconds is double the default http shutdown wait on purpose — the exit handler is the last resort, not the first — and it is a package constant rather than a tunable because this package cannot read the configuration: the logger it builds is what the configuration is loaded through. It stands in for a budget its caller did not declare; a caller that declares one is honoured on this door too. */
var exitStepBudget = 10 * time.Second

/* RunShieldedStep is the exit handler's own shield, offered to the one other caller that stands between a process and its end: the normal return of Run, whose teardown is deferred with no budget at all, so the healthy shutdown was the one without an emergency exit while the panicking one had a ten-second escape. It contains a panic inside the step, echoes it to stderr best-effort, and abandons a step that does not return within the budget, answering whether the step ran to its end. A caller that gets false has a process holding something it cannot release and should end rather than wait; a contained panic answers false for the same reason, because the step stopped where it raised.

   The step keeps running on its goroutine after abandonment, so anything it writes must not be read by a caller that was told it did not finish.

   The step is handed a context carrying the deadline the shield holds it to, so what it drives can end itself in time to say what happened rather than being cut off mid-sentence. */
func RunShieldedStep(stepName string, step func(stepContext context.Context)) bool {
    return runExitStepShielded(stepName, step)
}

/* RunShieldedStepWithin is RunShieldedStep under a budget its caller declares, for the one caller that knows what its own teardown costs: the process that is shutting down cleanly, whose services carry close budgets of their own that this package cannot see. A non-positive budget is not a missing one — it is the caller saying there is to be NO deadline, and the step is then waited out however long it takes, which is what an operator asks for when the answer to "how long may this take" is "as long as the slowest component needs".

   The two budgets are deliberately different and stay that way. The panic path keeps the package constant when its caller declares none, because this package cannot read the configuration — the logger it builds is what the configuration is loaded through — and because it is the last resort, where waiting longer buys a dying process nothing. The clean path takes what its caller declares, because a caller that has a configuration knows whether ten seconds is longer than everything it must release or shorter than one of them. Sizing them to each other would answer one of those two questions with the other's answer. */
func RunShieldedStepWithin(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
    return runExitStepShieldedWithin(budget, stepName, step)
}

func runExitStepShielded(stepName string, step func(stepContext context.Context)) bool {
    return runExitStepShieldedWithin(exitStepBudget, stepName, step)
}

/* stepDeadlineWithin answers the deadline the step is given, which is deliberately EARLIER than the moment the shield abandons it. A step told to finish at the same instant the shield gives up is abandoned every time, not sometimes: the timer is armed before the step starts, so a step that honours exactly the budget it was handed returns after the timer has already fired — measured at forty runs out of forty, against forty out of forty completed once the step's deadline sat below the shield's. The abandoned step then finishes its work a moment later with nobody left to receive it, and os.Exit ends the process before it can be written anywhere, which is the whole of what an operator would have learned.

   The budget is therefore spent in two halves: the first is the deadline the step is held to, the second is the headroom in which a step that honoured it is allowed to say so. Both come from the one figure the caller declared, so the declared value stays the ceiling for the whole thing — which is what a supervisor's termination grace is measured against. The audit storage spends its own close grace twice for the same reason, once to drain and once to wait for the reaction to the cancellation it then sends. */
func stepDeadlineWithin(budget time.Duration) time.Duration {
    return budget / 2
}

/* runExitStepShieldedWithin contains a panic inside one step of the exit handler and echoes it to stderr best-effort, and abandons a step that does not return within the budget it is given: the steps stand between a fatal failure and os.Exit, so a teardown blocked on a close that never returns — a drain on an unbuffered channel, a lock somebody died holding — would otherwise turn a dying process into a hung one, with the record written and the exit never taken. The budget is a parameter rather than the package constant because the two callers know different things about how long a step may legitimately take: the exit handler knows only that it is the last resort, while a process shutting down cleanly can be told by its configuration what its own services cost to release. The step keeps running on its goroutine after abandonment; os.Exit ends it with the process. It answers whether the step ran to its end: a step abandoned on the budget and a step whose panic was contained here both left work undone, and a caller that is told otherwise records a teardown that never happened. */
func runExitStepShieldedWithin(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
    /* the channel carries the outcome rather than only the fact that the goroutine ended, because closing it alone reported a recovered panic as a completed step; it is buffered so the send cannot park forever once the budget has abandoned the step and nobody is left to receive */
    stepDone := make(chan bool, 1)

    /* a non-positive budget hands the step a context with no deadline, the same absence the select below is given: the caller said there is no term, and a context carrying one would put back the term the caller removed */
    stepContext := context.Background()

    if 0 < budget {
        deadlineContext, cancelStepContext := context.WithTimeout(stepContext, stepDeadlineWithin(budget))
        defer cancelStepContext()

        stepContext = deadlineContext
    }

    go func() {
        stepCompleted := false

        /* the recover lives on the step's own goroutine: a recover in the waiting parent could never catch a panic raised here */
        defer func() {
            recoveredValue := recover()
            if nil != recoveredValue {
                _, _ = fmt.Fprintf(os.Stderr, "melody: panic while %s during the exit handler: %v\n", stepName, recoveredValue)
            }

            stepDone <- stepCompleted
        }()

        step(stepContext)

        stepCompleted = true
    }()

    /* a non-positive budget leaves this channel nil, and a receive on a nil channel blocks for ever, so the select has only one case that can ever fire: the step is waited out to its end. It is written as an absent case rather than as a very large duration because "no deadline" is what the caller said, and a figure large enough to stand in for it is still a figure somebody has to defend. */
    var budgetExpired <-chan time.Time
    if 0 < budget {
        budgetExpired = time.After(budget)
    }

    select {
    case stepCompleted := <-stepDone:
        return stepCompleted

    case <-budgetExpired:
        _, _ = fmt.Fprintf(os.Stderr, "melody: %s did not return within %s during the exit handler; abandoning it\n", stepName, budget)

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
        /* the value reaching this line came out of a recover, so its Error() can be the very dereference that made it panic-worthy; LogContext renders it under a recover, which is the same door the record above was written through */
        rendered, isRendered := exception.LogContext(err)["error"].(string)
        if true == isRendered && "" != rendered {
            message = rendered
        }
    }

    _, _ = fmt.Fprintf(os.Stderr, "melody: exiting with code %d after unrecovered error: %s\n", exitCode, message)
}
