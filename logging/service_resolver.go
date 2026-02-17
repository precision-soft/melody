package logging

import (
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
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

func LoggerFromResolver(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
	return container.FromResolver[loggingcontract.Logger](resolver, ServiceLogger)
}
