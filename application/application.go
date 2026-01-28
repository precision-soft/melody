package application

import (
	"context"
	"io/fs"

	applicationcontract "github.com/precision-soft/melody/application/contract"
	clicontract "github.com/precision-soft/melody/cli/contract"
	"github.com/precision-soft/melody/config"
	configcontract "github.com/precision-soft/melody/config/contract"
	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	"github.com/precision-soft/melody/logging"
	"github.com/precision-soft/melody/security"
)

type RouteRegistrar func(kernelInstance kernelcontract.Kernel)

type Application struct {
	booted                bool
	configuration         configcontract.Configuration
	runtimeFlags          *RuntimeFlags
	kernel                kernelcontract.Kernel
	embeddedPublicFiles   fs.FS
	modules               []applicationcontract.Module
	cliCommands           []clicontract.Command
	httpRouteRegistrars   []RouteRegistrar
	httpMiddlewares       *HttpMiddleware
	securityConfiguration *security.CompiledConfiguration
	routeRegistry         httpcontract.RouteRegistry
}

func (instance *Application) Boot() kernelcontract.Kernel {
	if true == instance.booted {
		return instance.kernel
	}

	defer instance.logOnRecoverAndExit()

	configuration := instance.configuration

	instance.bootModules()

	resolveErr := configuration.Resolve()
	if nil != resolveErr {
		exception.Panic(
			exception.NewError("could not resolve the config parameters on boot", nil, resolveErr),
		)
	}

	instance.ensureRuntimeDirectories()

	instance.bootContainer()

	instance.bootCli()

	instance.bootHttp()

	instance.booted = true

	return instance.kernel
}

func (instance *Application) RegisterParameter(
	name string,
	value any,
) {
	if true == instance.booted {
		exception.Panic(
			exception.NewError(
				"cannot register parameter after application boot",
				exceptioncontract.Context{
					"parameterName": name,
				},
				nil,
			),
		)
	}

	instance.configuration.RegisterRuntime(name, value)
}

func (instance *Application) Run(ctx context.Context) {
	_ = instance.Boot()

	defer instance.logOnRecoverAndExit()

	defer instance.Close()

	if config.ModeCli == instance.runtimeFlags.Mode() {
		stripRuntimeFlagsFromOsArgs()

		runCliErr := instance.runCli(ctx)
		if nil != runCliErr {
			exitError, ok := runCliErr.(*exception.ExitError)
			if true == ok {
				exception.Exit(exitError)
			}

			exception.Panic(
				exception.FromError(runCliErr),
			)
		}

		return
	}

	runHttpErr := instance.runHttp(ctx)
	if nil != runHttpErr {
		exception.Panic(
			exception.FromError(runHttpErr),
		)
	}
}

func (instance *Application) ensureRuntimeDirectories() {
	configuration := instance.configuration

	projectDirectory := configuration.Kernel().ProjectDir()
	logsDirectory := configuration.Kernel().LogsDir()
	cacheDirectory := configuration.Kernel().CacheDir()

	ensureRuntimeDirectoriesErr := ensureRuntimeDirectories(
		projectDirectory,
		logsDirectory,
		cacheDirectory,
	)
	if nil != ensureRuntimeDirectoriesErr {
		exception.Panic(
			exception.NewError(
				"failed to create runtime directories",
				exceptioncontract.Context{
					"projectDirectory": projectDirectory,
					"logsDirectory":    logsDirectory,
					"cacheDirectory":   cacheDirectory,
				},
				ensureRuntimeDirectoriesErr,
			),
		)
	}
}

func (instance *Application) logOnRecoverAndExit() {
	recovered := recover()
	if nil == recovered {
		return
	}

	logger := logging.EmergencyLogger()

	serviceContainer := instance.kernel.ServiceContainer()

	containerLogger, loggerErr := logging.LoggerFromContainer(serviceContainer)
	if nil == loggerErr && nil != containerLogger {
		logger = containerLogger
	}

	logging.LogOnRecoverAndExit(logger, recovered, 1)
}
