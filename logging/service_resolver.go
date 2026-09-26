package logging

import (
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

const (
    ServiceLogger = "service.logger"
)

func LoggerMustFromRuntime(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    return runtime.MustFromRuntime[loggingcontract.Logger](runtimeInstance, ServiceLogger)
}

/* LoggerFromRuntime resolves the logger and answers nil when it cannot, recording the failure through the emergency logger. A typed nil, which the container refuses today, is answered nil too, since it would panic on the first call. */
func LoggerFromRuntime(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    loggerInstance, err := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, ServiceLogger)
    if nil != err {
        EmergencyLogger().Emergency(
            "could not get the logger from runtime",
            exception.LogContext(
                err,
                exceptioncontract.Context{
                    "service": ServiceLogger,
                },
            ),
        )

        return nil
    }

    if nil == loggerInstance || true == internal.IsNilInterface(loggerInstance) {
        EmergencyLogger().Emergency(
            "the logger resolved from runtime is nil",
            exceptioncontract.Context{
                "service": ServiceLogger,
            },
        )

        return nil
    }

    return loggerInstance
}

func LoggerMustFromContainer(serviceContainer containercontract.Container) loggingcontract.Logger {
    return container.MustFromResolver[loggingcontract.Logger](serviceContainer, ServiceLogger)
}

func LoggerFromContainer(serviceContainer containercontract.Container) (loggingcontract.Logger, error) {
    return container.FromResolver[loggingcontract.Logger](serviceContainer, ServiceLogger)
}

func LoggerMustFromResolver(resolver containercontract.Resolver) loggingcontract.Logger {
    return container.MustFromResolver[loggingcontract.Logger](resolver, ServiceLogger)
}

func LoggerFromResolver(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
    return container.FromResolver[loggingcontract.Logger](resolver, ServiceLogger)
}
