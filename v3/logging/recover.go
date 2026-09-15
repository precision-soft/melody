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

            exception.Exit(exitError)
        }

        return
    }

    if true == isAlreadyLoggedValue(recoveredValue) {
        if true == panicAgain {

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

    LogOnRecoverAndExitAfter(logger, recovered, exitCode, exitStepBudget, nil)
}

/* LogOnRecoverAndExitAfter logs the recovered value, runs beforeExit under the supplied budget, then exits. Non-positive budgets use the panic-path package fallback. The hook runs before process exit because os.Exit skips deferred teardown. */
func LogOnRecoverAndExitAfter(
    logger loggingcontract.Logger,
    recovered any,
    exitCode int,
    beforeExitBudget time.Duration,
    beforeExit func(beforeExitContext context.Context),
) {

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

    if true == needsLogging {
        runExitStepShielded("logging the exit record", func(_ context.Context) {
            LogError(logger, err)
            err.MarkAsLogged()
        })
    }

    runExitStepShielded("logging the exit certificate", func(_ context.Context) {
        writeExitCertificate(logger, err, resolvedExitCode)
    })

    if nil != beforeExit {
        runExitStepShieldedWithin(exitPathStepBudget(beforeExitBudget), "running the before-exit hook", beforeExit)
    }

    echoDone := make(chan struct{})

    go func() {
        defer close(echoDone)

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

var exitStepBudget = 10 * time.Second

func exitPathStepBudget(declaredBudget time.Duration) time.Duration {
    if 0 < declaredBudget {
        return declaredBudget
    }

    return exitStepBudget
}

/* RunShieldedStep contains panics and bounds waiting with the package budget. It returns false for panic or timeout. Abandoned code may continue on its goroutine, so callers must not read its mutable results. The step receives an earlier deadline within the outer budget to allow completion reporting. */
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

func stepDeadlineWithin(budget time.Duration) time.Duration {
    return budget / 2
}

func runExitStepShieldedWithin(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {

    stepDone := make(chan bool, 1)

    stepContext := context.Background()

    if 0 < budget {
        deadlineContext, cancelStepContext := context.WithTimeout(stepContext, stepDeadlineWithin(budget))
        defer cancelStepContext()

        stepContext = deadlineContext
    }

    go func() {
        stepCompleted := false

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

func resolveRecoveredExit(
    recovered any,
    exitCode int,
) (*exception.Error, int, bool) {
    exitError, isExitError := recovered.(*exception.ExitError)
    if true == isExitError && nil != exitError {
        ownExitCode := exitError.ExitCode()

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

func isAlreadyLoggedValue(recovered any) bool {
    err, isError := recovered.(error)
    if false == isError {
        return false
    }

    return exception.IsAlreadyLogged(err)
}

func echoExitToStderr(err error, exitCode int) {
    if 0 == exitCode {
        return
    }

    message := "-"
    if nil != err {

        rendered, isRendered := exception.LogContext(err)["error"].(string)
        if true == isRendered && "" != rendered {
            message = rendered
        }
    }

    _, _ = fmt.Fprintf(os.Stderr, "melody: exiting with code %d after unrecovered error: %s\n", exitCode, message)
}
