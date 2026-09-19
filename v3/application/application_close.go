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

/* closeBeforeExit is the shape the exit handler's before-exit hook takes: it carries the deadline the shield holds the teardown to, so the services being released can end themselves in time for the failure they report to reach the record the handler is about to write. */
func (instance *Application) closeBeforeExit(closeContext context.Context) {
    _ = instance.close(closeContext)
}

/* close tears the application down and returns the teardown failure only when this call was the one that discovered it. A container somebody else already closed hands its memoized error to every later Close; re-reporting it here would present one failure as two incidents, and its exit code already belongs to whoever performed that close.

   Only the claim's winner enters the container at all. When every racing sibling called the container's Close too, whichever sibling arrived FIRST was the one whose call ran the actual teardown — the container serializes on its own once — and when that first arrival was a claim LOSER, the winner then read the closedness probe as "somebody else's close" and suppressed the report: the single failure was reported by nobody, and an exit path gated on it proceeded over a failed teardown. A loser now waits for the performer's whole teardown instead, so the probe's answer can only mean a close that genuinely came from outside this application. */
func (instance *Application) close(closeContext context.Context) error {
    /* a boot that died before the kernel was assembled has nothing to tear down: the exit handler now runs this close as its before-exit hook, and dereferencing the absent kernel there would replace a clean exit with a panic inside the one handler that must not panic. The check reads through the interface, since a typed nil passes a plain comparison and reaches the same dereference. */
    if true == internal.IsNilInterface(instance.kernel) {
        return nil
    }

    doneChannel := instance.closeDoneChannel()

    if false == instance.closePerformerClaimed.CompareAndSwap(false, true) {
        <-doneChannel

        return nil
    }

    defer close(doneChannel)

    emergencyLogger := logging.EmergencyLogger()

    serviceContainer := instance.kernel.ServiceContainer()

    alreadyClosed := false
    closedChecker, isChecker := serviceContainer.(interface{ IsClosed() bool })
    if true == isChecker {
        alreadyClosed = closedChecker.IsClosed()
    }

    serviceContainerCloseErr := closeServiceContainerWithin(closeContext, serviceContainer)

    if nil != serviceContainerCloseErr && false == alreadyClosed {
        emergencyLogger.Emergency("failed to close service container", exception.LogContext(serviceContainerCloseErr))

        logging.CloseEmergencyLogger()

        return serviceContainerCloseErr
    }

    /* a teardown that ran past its deadline and failed nothing is a diagnostic, not a failure: it returns nil and exits clean, and the record of who spent the budget would be lost with it — the only close that could have named the service was the one that answered nil. The container keeps that record behind a door, read here under the same discoverer-only rule as the failure above, and written to the same journal as a warning; a Container implementation without the door has nothing to say, and a close with no deadline leaves no record. */
    if false == alreadyClosed {
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

/* closeDoneChannel builds the performer-done channel on first use: the Application is constructed by literal in half its own suite, so an eagerly constructed channel would be nil exactly there. */
func (instance *Application) closeDoneChannel() chan struct{} {
    instance.closeDoneOnce.Do(func() {
        instance.closeDone = make(chan struct{})
    })

    return instance.closeDone
}

/* closeServiceContainerWithin prefers the container's context-taking teardown when it has one, exactly the way this file already discovers IsClosed on the same value: the contract declares Close alone, so a method added to it would be a method every application carrying its own Container implementation would have to grow, and the door is reached by asking the value instead. A container that carries only Close is closed with it, and the budget then bounds the shield around this step rather than the closes inside it — which is the state every container was in before the door existed. */
func closeServiceContainerWithin(closeContext context.Context, serviceContainer containercontract.Container) error {
    contextCloser, isContextCloser := serviceContainer.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if true == isContextCloser {
        return contextCloser.CloseWithContext(closeContext)
    }

    return serviceContainer.Close()
}
