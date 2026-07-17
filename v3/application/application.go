package application

import (
    "context"
    "io/fs"
    "os"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/security"
)

type RouteRegistrar func(kernelInstance kernelcontract.Kernel)

type Application struct {
    booted                bool
    ctx                   context.Context
    configuration         configcontract.Configuration
    runtimeFlags          *RuntimeFlags
    kernel                kernelcontract.Kernel
    embeddedPublicFiles   fs.FS
    modules               []applicationcontract.Module
    cliCommands           []clicontract.Command
    httpRouteRegistrars   []RouteRegistrar
    httpMiddlewares       *HttpMiddleware
    httpHandlerDecorators []applicationcontract.HttpHandlerDecorator
    httpShutdownHooks     []func()
    securityConfiguration *security.CompiledConfiguration
    routeRegistry         httpcontract.RouteRegistry
    moduleConfigurations  map[string]any
    bootCollisions        []bootCollision
}

func (instance *Application) Boot() kernelcontract.Kernel {
    if true == instance.booted {
        return instance.kernel
    }

    defer instance.logOnRecoverAndExit()

    configuration := instance.configuration

    instance.bootModulesPreConfigurationResolve()

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        /* name the project directory in the failure: an unresolved parameter usually means the .env artifacts were not found there — melody derives the directory from the executable location (the working directory under go run), so a binary run from elsewhere fails exactly here with an otherwise unsuggestive "undefined environment key" */
        projectDirectory := ""
        if projectDirectoryParameter := configuration.Get(config.KernelProjectDir); nil != projectDirectoryParameter {
            projectDirectory = projectDirectoryParameter.String()
        }

        exception.Panic(
            exception.NewError(
                "could not resolve the config parameters on boot"+missingEnvironmentFileHint(projectDirectory),
                exceptioncontract.Context{
                    "projectDirectory": projectDirectory,
                },
                resolveErr,
            ),
        )
    }

    instance.ensureRuntimeDirectories()

    instance.bootModulesPostConfigurationResolve()

    instance.bootContainer()

    warnIgnoredProcessEnvironment(instance.bootLogger(), configuration, os.Environ())

    instance.bootCli()

    instance.panicOnBootCollisions()

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

    /* a duplicate is recorded for the aggregated boot report instead of panicking one at a time; the first registration wins until the guaranteed panic ends the boot */
    if "" != name && nil != instance.configuration.Get(name) {
        instance.recordBootCollision(bootCollisionKindParameter, name, 1)
        return
    }

    instance.configuration.RegisterRuntime(name, value)
}

/* Configuration exposes the loaded configuration before boot so wiring code (module
   construction in the composition root) can read parameters — including the values
   melody auto-registers from the .env files — without reaching for os.Getenv. Services
   resolved from the container should instead read config through the resolver. */
func (instance *Application) Configuration() configcontract.Configuration {
    return instance.configuration
}

/* ProcessRole is the resolved process role (config.RoleWeb, config.RoleWorker or config.RoleAll): an explicit --role flag wins over the MELODY_PROCESS_ROLE parameter, which defaults to all. Melody gates nothing on it — wiring code queries it to decide whether to register background runners (outbox relays, consumers) on this process; services resolve the same value through ServiceProcessRole. */
func (instance *Application) ProcessRole() string {
    return instance.runtimeFlags.Role()
}

func (instance *Application) Run() {
    _ = instance.Boot()

    defer instance.logOnRecoverAndExit()

    defer instance.Close()

    if config.ModeCli == instance.runtimeFlags.Mode() {
        stripRuntimeFlagsFromOsArgs()

        runCliErr := instance.runCli()
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

    runHttpErr := instance.runHttp(instance.ctx)
    if nil != runHttpErr {
        exception.Panic(
            exception.FromError(runHttpErr),
        )
    }
}

func (instance *Application) RegisterConfiguration(name string, configuration any) {
    if true == instance.booted {
        exception.Panic(
            exception.NewError(
                "cannot register configuration after application boot",
                exceptioncontract.Context{
                    "configurationName": name,
                },
                nil,
            ),
        )
    }

    if "" == name {
        exception.Panic(
            exception.NewError("cannot register configuration with empty name", nil, nil),
        )
    }

    _, exists := instance.moduleConfigurations[name]
    if true == exists {
        /* recorded for the aggregated boot report instead of panicking one at a time; the first registration wins until the guaranteed panic ends the boot */
        instance.recordBootCollision(bootCollisionKindConfiguration, name, 1)
        return
    }

    instance.moduleConfigurations[name] = configuration
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
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    logger := logging.EmergencyLogger()

    serviceContainer := instance.kernel.ServiceContainer()

    containerLogger, loggerErr := logging.LoggerFromContainer(serviceContainer)
    if nil == loggerErr && nil != containerLogger {
        logger = containerLogger
    }

    logging.LogOnRecoverAndExit(logger, recoveredValue, 1)
}

var _ applicationcontract.ParameterRegistrar = (*Application)(nil)
var _ applicationcontract.ConfigRegistrar = (*Application)(nil)
