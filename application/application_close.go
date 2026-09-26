package application

import (
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/logging"
)

func (instance *Application) Close() {
    _ = instance.close()
}

/* close tears the application down and returns the teardown failure only when this call discovered it; a container someone else closed answers its memoized error, whose exit code belongs to that close. Only the claim's winner enters the container, and a loser waits for the winner's whole teardown, so the closedness probe can only mean a close from outside this application. */
func (instance *Application) close() error {
    /* a boot that died before the kernel was assembled has nothing to tear down, and this runs inside the exit handler that must not panic; read through the interface, since a typed nil passes a plain comparison */
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

/* closeDoneChannel builds the performer-done channel on first use, since an Application built by literal has none. */
func (instance *Application) closeDoneChannel() chan struct{} {
    instance.closeDoneOnce.Do(func() {
        instance.closeDone = make(chan struct{})
    })

    return instance.closeDone
}
