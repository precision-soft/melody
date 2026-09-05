package application

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
)

func (instance *Application) Close() {
    _ = instance.close()
}

/* close tears the application down and returns the teardown failure only when this call was the one that discovered it. A container somebody else already closed hands its memoized error to every later Close; re-reporting it here would present one failure as two incidents, and its exit code already belongs to whoever performed that close.

   Only the claim's winner enters the container at all. When every racing sibling called the container's Close too, whichever sibling arrived FIRST was the one whose call ran the actual teardown — the container serializes on its own once — and when that first arrival was a claim LOSER, the winner then read the closedness probe as "somebody else's close" and suppressed the report: the single failure was reported by nobody, and an exit path gated on it proceeded over a failed teardown. A loser now waits for the performer's whole teardown instead, so the probe's answer can only mean a close that genuinely came from outside this application. */
func (instance *Application) close() error {
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

    serviceContainerCloseErr := serviceContainer.Close()

    if nil != serviceContainerCloseErr && false == alreadyClosed {
        emergencyLogger.Emergency("failed to close service container", exception.LogContext(serviceContainerCloseErr))

        logging.CloseEmergencyLogger()

        return serviceContainerCloseErr
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
