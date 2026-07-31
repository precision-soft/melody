package application

import (
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/logging"
)

func (instance *Application) Close() {
    _ = instance.close()
}

/* close tears the application down and returns the teardown failure only when this call was the one that discovered it. A container somebody else already closed — the cli action closes it right after the command runs — hands its memoized error to every later Close; re-reporting it here would present one failure as two incidents, and its exit code already belongs to whoever performed that close, which folded it into the command's own result. */
func (instance *Application) close() error {
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
