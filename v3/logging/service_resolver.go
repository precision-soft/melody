package logging

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ServiceLogger = "service.logger"
)

func LoggerMustFromRuntime(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    return runtime.MustFromRuntime[loggingcontract.Logger](runtimeInstance, ServiceLogger)
}

func LoggerFromRuntime(runtimeInstance runtimecontract.Runtime) loggingcontract.Logger {
    loggerInstance, err := runtime.FromRuntime[loggingcontract.Logger](runtimeInstance, ServiceLogger)
    if nil == loggerInstance || nil != err {
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
        }

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
