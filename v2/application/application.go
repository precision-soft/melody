package application

import (
    "context"
    "errors"
    "io/fs"
    "os"

    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/security"
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
    moduleConfigurations  map[string]any
    bootCollisions        []bootCollision
    unappliedSecretMarks  []string
}

func (instance *Application) Boot() kernelcontract.Kernel {
    if true == instance.booted {
        return instance.kernel
    }

    defer instance.logOnRecoverAndExit()

    configuration := instance.configuration

    instance.bootModulesPreConfigurationResolve()

    /* the retry runs before the resolve on purpose: the marking must be on the parameter when the templates that read it resolve, or it would never travel into the derived values */
    instance.applyUnappliedSecretMarks()

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

    instance.warnUnappliedSecretMarks()

    instance.booted = true

    return instance.kernel
}

func (instance *Application) RegisterParameter(
    name string,
    value any,
) {
    instance.registerParameter(name, value, false)
}

/* RegisterSecretParameter declares a parameter holding a credential. It is registered and resolved like any other; the marking only keeps it, and every parameter whose template reads it, out of the rendered configuration. */
func (instance *Application) RegisterSecretParameter(
    name string,
    value any,
) {
    instance.registerParameter(name, value, true)
}

/* MarkParameterSecret marks a parameter that already exists — typically one melody registered automatically from the .env artifacts — as holding a credential. A name that matches nothing does not fail the boot, since an environment key is legitimately undefined in some environments; it is retried before the configuration resolves and again at the end of the boot, and warned about only then, so a misspelled name is visible instead of silently redacting nothing. */
func (instance *Application) MarkParameterSecret(name string) {
    if true == instance.booted {
        exception.Panic(
            exception.NewError(
                "cannot mark a parameter secret after application boot",
                exceptioncontract.Context{
                    "parameterName": name,
                },
                nil,
            ),
        )
    }

    if false == instance.configuration.MarkSecret(name) {
        instance.unappliedSecretMarks = append(instance.unappliedSecretMarks, name)
    }
}

/* applyUnappliedSecretMarks retries the markings that matched nothing when they were declared. It runs after every module registered its parameters and before the configuration resolves, so a retried marking still propagates into the parameters whose templates read the secret; what still matches nothing stays queued, since a later boot phase may yet register the parameter. */
func (instance *Application) applyUnappliedSecretMarks() {
    remaining := make([]string, 0, len(instance.unappliedSecretMarks))

    for _, name := range instance.unappliedSecretMarks {
        if false == instance.configuration.MarkSecret(name) {
            remaining = append(remaining, name)
        }
    }

    instance.unappliedSecretMarks = remaining
}

/* warnUnappliedSecretMarks runs when every boot phase that can register a parameter has finished: a marking that still matches nothing is a misspelled name or a key undefined in this environment, and the warning is what keeps it from silently redacting nothing. */
func (instance *Application) warnUnappliedSecretMarks() {
    for _, name := range instance.unappliedSecretMarks {
        if true == instance.configuration.MarkSecret(name) {
            continue
        }

        instance.bootLogger().Warning(
            "a secret marking matched no parameter; the name may be misspelled, or the environment key is undefined in this environment",
            loggingcontract.Context{
                "parameterName": name,
            },
        )
    }

    instance.unappliedSecretMarks = nil
}

func (instance *Application) registerParameter(
    name string,
    value any,
    isSecret bool,
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
        instance.recordBootCollision(bootCollisionKindParameter, name)
        return
    }

    if true == isSecret {
        instance.configuration.RegisterRuntimeSecret(name, value)

        return
    }

    instance.configuration.RegisterRuntime(name, value)
}

/* ProcessRole is the resolved process role (config.RoleWeb, config.RoleWorker or config.RoleAll): an explicit --role flag wins over the MELODY_PROCESS_ROLE parameter, which defaults to all. Melody gates nothing on it — wiring code queries it to decide whether to register background runners (outbox relays, consumers) on this process; services resolve the same value through ServiceProcessRole. */
func (instance *Application) ProcessRole() string {
    return instance.runtimeFlags.Role()
}

func (instance *Application) Run(ctx context.Context) {
    _ = instance.Boot()

    defer instance.logOnRecoverAndExit()

    defer instance.Close()

    if config.ModeCli == instance.runtimeFlags.Mode() {
        stripRuntimeFlagsFromOsArgs()

        runCliErr := instance.runCli(ctx)
        if nil != runCliErr {
            /* the exit-coded error may arrive wrapped — the cli action folds a command's error together with shutdown-close failures — so walk the cause chain rather than assert the top type, or an intended exit code degrades into a panic with a different code */
            var exitError *exception.ExitError
            if true == errors.As(runCliErr, &exitError) {
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
        instance.recordBootCollision(bootCollisionKindConfiguration, name)
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
