package application

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
)

func (instance *Application) Close() {
    _ = instance.close()
}

/* close tears the application down and returns the teardown failure only when this call was the one that discovered it. A container somebody else already closed hands its memoized error to every later Close; re-reporting it here would present one failure as two incidents, and its exit code already belongs to whoever performed that close. */
func (instance *Application) close() error {
    /* a boot that died before the kernel was assembled has nothing to tear down: the exit handler now runs this close as its before-exit hook, and dereferencing the absent kernel there would replace a clean exit with a panic inside the one handler that must not panic. The check reads through the interface, since a typed nil passes a plain comparison and reaches the same dereference. */
    if true == internal.IsNilInterface(instance.kernel) {
        return nil
    }

    emergencyLogger := logging.EmergencyLogger()

    serviceContainer := instance.kernel.ServiceContainer()

    /* the claim decides between two closes of THIS application racing each other; the probe still decides against a container somebody else closed directly, which the claim cannot see. Two concurrent closes both probe the container open, so without the claim both would read the memoized failure as their own discovery. */
    isClosePerformer := instance.closePerformerClaimed.CompareAndSwap(false, true)

    alreadyClosed := false
    closedChecker, isChecker := serviceContainer.(interface{ IsClosed() bool })
    if true == isChecker {
        alreadyClosed = closedChecker.IsClosed()
    }

    serviceContainerCloseErr := serviceContainer.Close()

    if nil != serviceContainerCloseErr && true == isClosePerformer && false == alreadyClosed {
        emergencyLogger.Emergency("failed to close service container", exception.LogContext(serviceContainerCloseErr))

        logging.CloseEmergencyLogger()

        return serviceContainerCloseErr
    }

    logging.CloseEmergencyLogger()

    return nil
}
