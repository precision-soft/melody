package application

import (
    "context"
    "errors"

    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
)

/* CloseWithContext tears the application down under the caller's context, whose deadline and cancel every phase of the close is handed, a closer that ignores it not cut at the deadline, and returns the teardown failure when this call discovered it. An application that runs its own server closes under the caller's context and its TeardownTimeout, the earlier ending it, and a failure past the configured bound names it. An application that handed its http handler out is wound down first, bounded by the http shutdown timeout or the caller's context, whichever ends earlier: its shutdown hooks are started unless BeginHttpShutdown started them, then awaited with its open request scopes; the teardown that follows runs under what is left of the caller's context, never a context of its own. */
func (instance *Application) CloseWithContext(closeContext context.Context) error {
    /* refused before the once the close claims, so a refused call leaves the teardown to a later one */
    if true == internal.IsNilInterface(closeContext) {
        return exception.NewError("the close context is nil", exceptioncontract.Context{"door": "Application.CloseWithContext"}, nil)
    }

    if nil != instance.handedOutHttpHandler() {
        return instance.close(closeContext)
    }

    teardownBudget := instance.teardownBudget()
    if true == teardownBudget.IsUnlimited() {
        return instance.close(closeContext)
    }

    teardownTimeout, _ := teardownBudget.Value()
    boundedContext, cancel := context.WithTimeout(closeContext, teardownTimeout)
    defer cancel()

    closeErr := instance.close(boundedContext)
    if nil == closeErr || nil == boundedContext.Err() || nil != closeContext.Err() {
        return closeErr
    }

    return exception.NewError(
        "the application's close ended at its configured bound",
        exceptioncontract.Context{"door": "Application.CloseWithContext", "bound": config.TeardownTimeoutKey},
        closeErr,
    )
}

/* closeBeforeExit is the exit handler's before-exit hook; it carries the deadline the shield holds the teardown to. */
func (instance *Application) closeBeforeExit(closeContext context.Context) {
    _ = instance.close(closeContext)
}

/* close tears the application down and returns the teardown failure only when this call discovered it; a container someone else closed answers its memoized error, whose exit code belongs to that close. Only the claim's winner enters the container, and a loser waits for the winner's whole teardown, so the closedness probe can only mean a close from outside this application. */
func (instance *Application) close(closeContext context.Context) error {
    /* a boot that died before the kernel was assembled has nothing to tear down, and this runs inside the exit handler that must not panic; read through the interface, since a typed nil passes a plain comparison */
    if true == internal.IsNilInterface(instance.kernel) {
        return nil
    }

    if nil != instance.finishBoot {
        _ = instance.finishBoot()
    }

    performed := false
    var performErr error

    instance.closePerformerOnce.Do(func() {
        performed = true
        performErr = instance.performClose(closeContext)
    })

    if false == performed {
        return nil
    }

    return performErr
}

/* performClose winds a handed-out http handler down before the container, whose services its requests and hooks still use, and joins the wind-down's failure with the teardown's; the teardown runs under what is left of the caller's context, so the caller's deadline and cancel reach every closer */
func (instance *Application) performClose(closeContext context.Context) error {
    windDownErr := instance.windDownHandedOutHttpHandler(closeContext)

    teardownErr := instance.performTeardown(closeContext)

    if nil == windDownErr {
        return teardownErr
    }

    if nil == teardownErr {
        return windDownErr
    }

    return errors.Join(windDownErr, teardownErr)
}

/* TeardownWalkBudgetEndedMessage is the warning the close writes when the node budget of ContainerConfig.TeardownWalk ended the walk of a service, so the waves may close it before a collaborator it holds. */
const TeardownWalkBudgetEndedMessage = "the teardown walk budget ended before the walk of a service did; an edge past it was not gained"

func (instance *Application) performTeardown(closeContext context.Context) error {
    emergencyLogger := logging.EmergencyLogger()

    serviceContainer := instance.kernel.ServiceContainer()

    alreadyClosed := false
    closedChecker, isChecker := serviceContainer.(interface{ IsClosed() bool })
    if true == isChecker {
        alreadyClosed = closedChecker.IsClosed()
    }

    /* the walk that infers the teardown edges is bounded, so a service whose walk the budget ended is named before the teardown that misses its edges */
    if false == alreadyClosed {
        budgetReporter, reportsBudget := serviceContainer.(interface{ TeardownWalkBudgetEnded() []string })
        if true == reportsBudget {
            if budgetEnded := budgetReporter.TeardownWalkBudgetEnded(); 0 < len(budgetEnded) {
                emergencyLogger.Warning(
                    TeardownWalkBudgetEndedMessage,
                    exceptioncontract.Context{"services": budgetEnded, "field": "ContainerConfig.TeardownWalk.Nodes"},
                )
            }
        }
    }

    serviceContainerCloseErr := serviceContainer.CloseWithContext(closeContext)

    /* a container already closed before this close reached it answers for a teardown somebody else performed: neither its failure nor its deadline record is this close's to report */
    if false == alreadyClosed {
        if nil != serviceContainerCloseErr {
            emergencyLogger.Emergency("failed to close service container", exception.LogContext(serviceContainerCloseErr, exception.LogContextOptions{}))

            logging.CloseEmergencyLogger()

            return serviceContainerCloseErr
        }

        /* a teardown that overran its deadline and failed nothing returns nil, so the container's record of who spent the budget is read here, under the same discoverer-only rule, and written as a warning; a Container without that door, or a close with no deadline, leaves no record */
        overrunReporter, reportsOverrun := serviceContainer.(interface {
            TeardownDeadlineOverrun() exceptioncontract.Context
        })
        if true == reportsOverrun {
            if overrun := overrunReporter.TeardownDeadlineOverrun(); nil != overrun {
                emergencyLogger.Warning("service container teardown overran its deadline", overrun)
            }
        }
    }

    logging.CloseEmergencyLogger()

    return nil
}

