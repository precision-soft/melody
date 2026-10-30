package logging

import (
    "context"
    "fmt"
    "os"
    "runtime/debug"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* LogOnRecover recovers the panic in flight, logs it, and panics again when panicAgain is set. It never terminates the process, which would skip the teardown above it; a recovered *exception.ExitError is re-panicked unchanged when panicAgain is set, so its code reaches the process boundary, and is logged and swallowed like any other panic when it is not; LogOnRecoverAndExit is the helper that exits. */
func LogOnRecover(
    logger loggingcontract.Logger,
    panicAgain bool,
) {
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    if exitError, isExitError := internal.RecoveredExitError(recoveredValue); true == isExitError {
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
            /* the recovered value is re-panicked unchanged, since the mark that suppressed this record lives on it */
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
                recoveredErrorMessage(value),
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

/* recoveredErrorMessage reads a recovered error's message under a recover of its own: it runs in the recovery defers of the process boundary, where an Error() that dereferences a nil left by the first panic would raise a second past the teardown and the exit code. */
func recoveredErrorMessage(value error) (text string) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        text = "error message panicked: " + describeRecoveredValue(recoveredValue)
    }()

    return value.Error()
}

/* describeRecoveredValue names a recovered panic value by its own Error or String, which run inside the defers reporting a failure. A value whose naming panics is named by its type, which calls none of its methods. */
func describeRecoveredValue(value any) (text string) {
    defer func() {
        if nil == recover() {
            return
        }

        text = fmt.Sprintf("a value of type %T whose rendering panicked", value)
    }()

    return renderTextValue(value)
}

/* newRecoveredPanicError wraps a panic payload that carries no usable error with the stack of the panic still in flight, the one moment the origin of a runtime panic can be captured. */
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

/* ExitConfig configures the step LogOnRecoverAndExit runs between the exit record and os.Exit; the zero value runs no hook. */
type ExitConfig struct {
    /* BeforeExit runs between the exit record and os.Exit, the one place after the record and before the exit; nil for none. */
    BeforeExit func(beforeExitContext context.Context)

    /* BeforeExitBudget bounds BeforeExit, as the clean return bounds its teardown; 10s when left out, at least 1ms, unlimited refused since this path must still reach os.Exit. */
    BeforeExitBudget bound.Duration
}

/* LogOnRecoverAndExit logs the recovered value, runs the config's hook under its budget and exits with exitCode. The exit code, refused outside 1..255, and the budget, refused by name outside its rule, are checked before the no-panic return, so a bad one is caught on the first healthy pass. */
func LogOnRecoverAndExit(
    logger loggingcontract.Logger,
    recovered any,
    exitCode int,
    config ExitConfig,
) {
    /* the code is checked as NewExitError checks it, since zero would make os.Exit report success; the check runs before the no-panic return, so a bad code is caught on the first healthy pass */
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

    beforeExitBudget, beforeExitBudgetErr := bound.Resolve(
        config.BeforeExitBudget,
        bound.Rule[time.Duration]{
            Name:    "ExitConfig.BeforeExitBudget",
            Default: exitStepBudget,
            Minimum: bound.TimeoutMinimum,
        },
    )
    if nil != beforeExitBudgetErr {
        exception.Panic(exception.FromError(beforeExitBudgetErr))
    }

    if nil == recovered {
        return
    }

    err, resolvedExitCode, needsLogging := resolveRecoveredExitShielded(recovered, exitCode)

    /* every step between the recovery and the exit runs under its own recover, so a second panic costs only its own step, never the exit code, the echo or os.Exit */
    if true == needsLogging {
        runExitStepShielded("logging the exit record", func(_ context.Context) {
            LogError(logger, err)
            err.MarkAsLogged()
        })
    }

    /* the certificate is written always, at emergency level, since the detailed record at the error's own level can be dropped by a threshold while still marked logged; it says the process is exiting and with what code */
    runExitStepShielded("logging the exit certificate", func(_ context.Context) {
        writeExitCertificate(logger, err, resolvedExitCode)
    })

    if nil != config.BeforeExit {
        runExitStepShieldedWithin(beforeExitBudget.Value, "running the before-exit hook", config.BeforeExit)
    }

    /* the echo leaves a trace on the standard streams when the record went to a file. It runs under the package budget, not the hook's, and outside the shield, since the shield reports on stderr, the very channel this step can block on; an abandoned echo is followed by os.Exit, silently. */
    echoDone := make(chan struct{})

    go func() {
        defer close(echoDone)

        /* a panic here would replace the exit code the process is owed; the echo is best-effort and the record is already written */
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

/* exitStepBudget is how long one step of the exit handler may run before it is abandoned, and the default of the budgets a caller leaves out; tests replace it. It is a variable of this package because the package cannot read the configuration, which is loaded through the logger it builds. */
var exitStepBudget = 10 * time.Second

/* ShieldedStepConfig configures RunShieldedStep; the zero value is the framework's. */
type ShieldedStepConfig struct {
    /* Budget bounds the step, its context's deadline falling at half of it; 10s when left out, at least 1ms, unlimited waits the step out with no deadline, as a process shutting down cleanly may. */
    Budget bound.Duration
}

/* RunShieldedStep runs a step under the exit handler's shield, containing a panic, echoing it to stderr best-effort, and abandoning a step that outlasts the budget. It answers whether the step ran to its end; false means the process holds something it cannot release and should end. The step keeps running after abandonment, and it is handed a context whose deadline falls at half the budget, so it can report before the shield abandons it. A budget outside its rule is not panicked, since the shield exists so nothing on the exit path panics: the refusal is echoed to stderr and the step runs under 10s. */
func RunShieldedStep(stepName string, config ShieldedStepConfig, step func(stepContext context.Context)) bool {
    budget, budgetErr := bound.Resolve(
        config.Budget,
        bound.Rule[time.Duration]{
            Name:              "ShieldedStepConfig.Budget",
            Default:           exitStepBudget,
            Minimum:           bound.TimeoutMinimum,
            UnlimitedAdmitted: true,
        },
    )
    if nil != budgetErr {
        /* echoed beside the step, so a stderr nobody drains cannot hold it; the echo is waited for at most the package budget */
        echoDone := make(chan struct{})

        go func() {
            defer close(echoDone)

            _, _ = fmt.Fprintf(os.Stderr, "melody: %s refused ShieldedStepConfig.Budget (%s) and runs under %s\n", stepName, internal.EscapeControlCharacters(budgetErr.Error()), exitStepBudget)
        }()

        completed := runExitStepShielded(stepName, step)

        select {
        case <-echoDone:

        case <-time.After(exitStepBudget):
        }

        return completed
    }

    /* an unlimited budget reaches the shield as the non-positive budget it waits out */
    if true == budget.Unlimited {
        return runExitStepShieldedWithin(0, stepName, step)
    }

    return runExitStepShieldedWithin(budget.Value, stepName, step)
}

func runExitStepShielded(stepName string, step func(stepContext context.Context)) bool {
    return runExitStepShieldedWithin(exitStepBudget, stepName, step)
}

/* stepDeadlineWithin answers a step deadline earlier than the moment the shield abandons it: the timer is armed before the step starts, so a step held to the whole budget would always be abandoned. The budget is split in two halves, the deadline and the headroom in which the step reports, both within the declared figure. */
func stepDeadlineWithin(budget time.Duration) time.Duration {
    return budget / 2
}

/* runExitStepShieldedWithin contains a panic inside one step and echoes it to stderr best-effort, and abandons a step that outlasts its budget, so a blocked close cannot turn a dying process into a hung one. It answers whether the step ran to its end; an abandoned step and a contained panic both left work undone. */
func runExitStepShieldedWithin(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
    /* the channel carries the outcome, so a recovered panic is not reported as a completed step; it is buffered so the send never parks once the budget abandoned the step */
    stepDone := make(chan bool, 1)

    /* a non-positive budget hands the step a context with no deadline, as the caller asked */
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
                _, _ = fmt.Fprintf(os.Stderr, "melody: panic while %s during the exit handler: %s\n", stepName, internal.EscapeControlCharacters(renderTextValue(recoveredValue)))
            }

            stepDone <- stepCompleted
        }()

        step(stepContext)

        stepCompleted = true
    }()

    /* a non-positive budget leaves this channel nil, whose receive blocks forever, so the step is waited out to its end */
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

/* resolveRecoveredExitShielded contains a recovered value whose own methods panic, which would otherwise unwind into main with the runtime's exit code and no record. A resolve that panics is answered with a generic record under the caller's exit code. */
func resolveRecoveredExitShielded(recovered any, exitCode int) (err *exception.Error, resolvedExitCode int, needsLogging bool) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        err = exception.NewEmergency(
            "recovered panic value could not be resolved",
            map[string]any{
                "resolvePanic": renderTextValue(recoveredValue),
            },
            nil,
        )
        resolvedExitCode = exitCode
        needsLogging = true
    }()

    return resolveRecoveredExit(recovered, exitCode)
}

/* resolveRecoveredExit answers the error the exit reports, the exit code, and whether the error still needs logging. An ExitError carries its own code, one holding no error is given one naming the anomaly, and a typed-nil exception is a plain panic value under the caller's code. */
func resolveRecoveredExit(
    recovered any,
    exitCode int,
) (*exception.Error, int, bool) {
    if exitError, isExitError := internal.RecoveredExitError(recovered); true == isExitError {
        ownExitCode := exitError.ExitCode()

        /* the rule NewExitError enforces, read again where the process ends: the zero value answers 0, which os.Exit reports as success, so a wrapper with an out-of-range code is normalized under the caller's code */
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
            /* latent: the range guard above intercepts the zero value, the one producer of a nil error, but this branch keeps the reader from dereferencing nil */
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
                recoveredErrorMessage(value),
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

/* writeExitCertificate writes the record that says the process is exiting and why, at emergency level, so it passes every threshold; the error travels in the context, since the record's subject is the exit. */
func writeExitCertificate(logger loggingcontract.Logger, err *exception.Error, exitCode int) {
    if true == internal.IsNilInterface(logger) {
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

/* isAlreadyLoggedValue answers whether a recovered payload carries the logged mark, read through exception.IsAlreadyLogged at the depth MarkLogged writes it; a payload that is not an error carries none. */
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
        /* a recovered value's Error() can be the dereference that made it panic, so it is rendered through LogContext, under a recover */
        rendered, isRendered := exception.LogContext(err)["error"].(string)
        if true == isRendered && "" != rendered {
            message = rendered
        }
    }

    _, _ = fmt.Fprintf(os.Stderr, "melody: exiting with code %d after unrecovered error: %s\n", exitCode, internal.EscapeControlCharacters(message))
}
