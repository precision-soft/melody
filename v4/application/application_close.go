package application

import (
    "context"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
)

func (instance *Application) Close() {
    _ = instance.close(context.Background())
}

/* CloseWithContext is Close under the caller's deadline, handed to the teardown, and returns the teardown failure when this call discovered it. */
func (instance *Application) CloseWithContext(closeContext context.Context) error {
    /* refused before the once the close claims, so a refused call leaves the teardown to a later one */
    if true == internal.IsNilInterface(closeContext) {
        return exception.NewError("the close context is nil", exceptioncontract.Context{"door": "Application.CloseWithContext"}, nil)
    }

    return instance.close(closeContext)
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

func (instance *Application) performClose(closeContext context.Context) error {
    emergencyLogger := logging.EmergencyLogger()

    serviceContainer := instance.kernel.ServiceContainer()

    alreadyClosed := false
    closedChecker, isChecker := serviceContainer.(interface{ IsClosed() bool })
    if true == isChecker {
        alreadyClosed = closedChecker.IsClosed()
    }

    serviceContainerCloseErr := serviceContainer.CloseWithContext(closeContext)

    /* a container already closed before this close reached it answers for a teardown somebody else performed: neither its failure nor its deadline record is this close's to report */
    if false == alreadyClosed {
        if nil != serviceContainerCloseErr {
            emergencyLogger.Emergency("failed to close service container", exception.LogContext(serviceContainerCloseErr, exception.LogContextConfig{}))

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

