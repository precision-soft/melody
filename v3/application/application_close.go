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

func (instance *Application) closeBeforeExit(closeContext context.Context) {
    _ = instance.close(closeContext)
}

func (instance *Application) close(closeContext context.Context) error {

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

func (instance *Application) closeDoneChannel() chan struct{} {
    instance.closeDoneOnce.Do(func() {
        instance.closeDone = make(chan struct{})
    })

    return instance.closeDone
}

func closeServiceContainerWithin(closeContext context.Context, serviceContainer containercontract.Container) error {
    contextCloser, isContextCloser := serviceContainer.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if true == isContextCloser {
        return contextCloser.CloseWithContext(closeContext)
    }

    return serviceContainer.Close()
}
