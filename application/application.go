package application

import (
    "context"
    "io/fs"
    "os"
    "sync"
    "sync/atomic"

    applicationcontract "github.com/precision-soft/melody/application/contract"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/security"
)

type RouteRegistrar func(kernelInstance kernelcontract.Kernel)

type Application struct {
    booted bool
    booting             bool
    configuration       configcontract.Configuration
    runtimeFlags        *RuntimeFlags
    kernel              kernelcontract.Kernel
    embeddedPublicFiles fs.FS
    modules             []applicationcontract.Module
    registeredModuleInstances map[applicationcontract.Module]struct{}
    cliCommands               []clicontract.Command
    httpRouteRegistrars       []RouteRegistrar
    httpMiddlewares           *HttpMiddleware
    securityConfiguration     *security.CompiledConfiguration
    routeRegistry             httpcontract.RouteRegistry
    moduleConfigurations      map[string]any
    bootCollisions            []bootCollision
    unappliedSecretMarks      []string
    unboundedDefaultCacheBackend bool
    defaultInMemorySessionStorage bool
    /* claimed by the one close that performs the teardown: two concurrent closes both probe the container open */
    closePerformerClaimed atomic.Bool

    /* a losing close waits on it for the whole teardown; built lazily, since tests construct the Application by literal */
    closeDoneOnce sync.Once
    closeDone     chan struct{}
}

func (instance *Application) Boot() kernelcontract.Kernel {
    if true == instance.booted {
        return instance.kernel
    }

    if true == instance.booting {
        exception.Panic(
            exception.NewError(
                "may not call Boot from inside a module boot hook",
                nil,
                nil,
            ),
        )
    }

    instance.booting = true

    defer instance.logOnRecoverAndExit()

    configuration := instance.configuration

    instance.bootModulesPreConfigurationResolve()

    /* the marking must be on the parameter when the templates that read it resolve */
    instance.applyUnappliedSecretMarks()

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
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

    instance.refuseHttpBootWithoutEnvironment()

    instance.ensureRuntimeDirectories()

    /* armed ahead of the first route and disarmed after the aggregated report, so every duplicate route lands in that report */
    instance.armRouteCollisionRecorder()

    /* the application's routes register ahead of any module's: the router breaks a dispatch tie on registration order */
    instance.bootHttp()

    instance.bootModulesPostConfigurationResolve()

    instance.bootContainer()

    warnIgnoredProcessEnvironment(instance.bootLogger(), configuration, os.Environ())

    instance.bootCli()

    instance.panicOnBootCollisions()

    instance.disarmRouteCollisionRecorder()

    instance.registerKernelHttpListeners()

    instance.warnUnappliedSecretMarks()

    instance.booted = true
    instance.booting = false

    return instance.kernel
}

func (instance *Application) RegisterParameter(
    name string,
    value any,
) {
    instance.registerParameter(name, value, false)
}

/* RegisterSecretParameter keeps it, and every parameter whose template reads it, out of the rendered configuration. */
func (instance *Application) RegisterSecretParameter(
    name string,
    value any,
) {
    instance.registerParameter(name, value, true)
}

/* MarkParameterSecret does not fail the boot on a name that matches nothing: it is retried, then warned about. */
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

func (instance *Application) applyUnappliedSecretMarks() {
    remaining := make([]string, 0, len(instance.unappliedSecretMarks))

    for _, name := range instance.unappliedSecretMarks {
        if false == instance.configuration.MarkSecret(name) {
            remaining = append(remaining, name)
        }
    }

    instance.unappliedSecretMarks = remaining
}

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

/* ProcessRole gates nothing, and nothing waits for runners: a runner that must finish observes the run context and drains. */
func (instance *Application) ProcessRole() string {
    return instance.runtimeFlags.Role()
}

func (instance *Application) Run(ctx context.Context) {
    _ = instance.Boot()

    markConfigurationServing(instance.configuration)

    /* record, then teardown, then exit: os.Exit runs no defer, and a deferred Close would close the record's logger */
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            instance.closeAndExitOnFailure()

            return
        }

        logging.LogOnRecoverAndExitAfter(instance.resolveExitLogger(), recoveredValue, 1, instance.Close)
    }()

    if config.ModeCli == instance.runtimeFlags.Mode() {
        stripRuntimeFlagsFromOsArgs()

        runCliErr := instance.runCli(ctx)
        if nil != runCliErr {
            exitError, isExitRequested := resolveCliExitError(runCliErr)
            if true == isExitRequested {
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

func resolveCliExitError(runCliErr error) (*exception.ExitError, bool) {
    var exitError *exception.ExitError

    if false == exception.ChainAs(runCliErr, &exitError) {
        return nil, false
    }

    /* the search reports success on a typed-nil link; it is answered as absent */
    if nil == exitError {
        return nil, false
    }

    return exitError, true
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

    if loggingcontract.LoggingConfigurationName != name {
        exception.Panic(
            exception.NewError(
                "unknown configuration name: nothing in this major consumes it",
                exceptioncontract.Context{
                    "configurationName": name,
                    "supportedNames":    []string{loggingcontract.LoggingConfigurationName},
                },
                nil,
            ),
        )
    }

    _, exists := instance.moduleConfigurations[name]
    if true == exists {
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

    logging.LogOnRecoverAndExitAfter(instance.resolveExitLogger(), recoveredValue, 1, instance.Close)
}

/* liveness is asked of the logger: the container serves built instances after Close, and a closed file logger drops writes */
func (instance *Application) resolveExitLogger() loggingcontract.Logger {
    logger := logging.EmergencyLogger()

    if true == internal.IsNilInterface(instance.kernel) {
        return instance.exitFileLogger(logger)
    }

    containerLogger, loggerErr := logging.LoggerFromContainer(instance.kernel.ServiceContainer())
    if nil != loggerErr || nil == containerLogger || true == internal.IsNilInterface(containerLogger) {
        return instance.exitFileLogger(logger)
    }

    closedChecker, isChecker := containerLogger.(interface{ Closed() bool })
    if true == isChecker && true == closedChecker.Closed() {
        return instance.exitFileLogger(logger)
    }

    return containerLogger
}

func (instance *Application) exitFileLogger(emergencyLogger loggingcontract.Logger) (logger loggingcontract.Logger) {
    logger = emergencyLogger

    defer func() {
        _ = recover()
    }()

    if nil == instance.configuration {
        return logger
    }

    kernelView := instance.configuration.Kernel()
    if nil == kernelView {
        return logger
    }

    return newContainerLogger(
        resolveRuntimePath(kernelView.ProjectDir(), kernelView.LogPath()),
        kernelView.LogLevel(),
        instance.moduleConfigurations,
    )
}

var applicationExit = os.Exit

var shieldedCloseStep = logging.RunShieldedStep

/* the error of an abandoned teardown is not read, since the step still runs on its own goroutine */
func (instance *Application) closeAndExitOnFailure() {
    var closeErr error

    completed := shieldedCloseStep("closing the application", func() {
        closeErr = instance.close()
    })

    if false == completed {
        applicationExit(1)

        return
    }

    if nil == closeErr {
        return
    }

    applicationExit(1)
}

/* an http process with no .env keys would serve in the dev environment with debug tooling on; cli stays permissive */
func (instance *Application) refuseHttpBootWithoutEnvironment() {
    if config.ModeHttp != instance.runtimeFlags.Mode() {
        return
    }

    counter, isCounter := instance.configuration.(environmentKeyCounter)
    if false == isCounter {
        return
    }

    if 0 < counter.EnvironmentKeyCount() {
        return
    }

    projectDirectory := instance.configuration.Kernel().ProjectDir()

    exception.Panic(
        exception.NewError(
            "no environment keys were loaded from the .env artifacts and the process is booting in http mode; refusing to serve on development defaults"+missingEnvironmentFileHint(projectDirectory),
            exceptioncontract.Context{
                "projectDirectory": projectDirectory,
            },
            nil,
        ),
    )
}

type environmentKeyCounter interface {
    EnvironmentKeyCount() int
}

type servingMarker interface {
    MarkServing()
}

func markConfigurationServing(configuration configcontract.Configuration) {
    marker, isMarker := configuration.(servingMarker)
    if false == isMarker {
        return
    }

    marker.MarkServing()
}

var _ applicationcontract.ParameterRegistrar = (*Application)(nil)
var _ applicationcontract.ConfigRegistrar = (*Application)(nil)
