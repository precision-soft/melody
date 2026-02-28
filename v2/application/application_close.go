package application

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/logging"
)

func (instance *Application) Close() {
    emergencyLogger := logging.EmergencyLogger()

    serviceContainerCloseErr := instance.kernel.ServiceContainer().Close()
    if nil != serviceContainerCloseErr {
        emergencyLogger.Emergency("failed to close service container", exception.LogContext(serviceContainerCloseErr))
    }

    logging.CloseEmergencyLogger()
}
