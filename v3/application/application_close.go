package application

import (
    "context"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
)

func (instance *Application) Close() {
    _ = instance.close(context.Background())
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

    serviceContainerCloseErr := closeServiceContainerWithin(closeContext, serviceContainer)

    /* a container already closed before this close reached it answers for a teardown somebody else performed: neither its failure nor its deadline record is this close's to report */
    if false == alreadyClosed {
        if nil != serviceContainerCloseErr {
            emergencyLogger.Emergency("failed to close service container", exception.LogContext(serviceContainerCloseErr))

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

/* closeServiceContainerWithin uses the container's context-taking teardown when the value has one, since the contract declares Close alone; a container with only Close is closed with it, and the budget then bounds the shield around this step. */
func closeServiceContainerWithin(closeContext context.Context, serviceContainer containercontract.Container) error {
    contextCloser, isContextCloser := serviceContainer.(containercontract.ContextCloser)
    if true == isContextCloser {
        return contextCloser.CloseWithContext(closeContext)
    }

    return serviceContainer.Close()
}
