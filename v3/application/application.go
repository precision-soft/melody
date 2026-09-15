package application

import (
    "context"
    "errors"
    "io/fs"
    "os"
    "sync"
    "sync/atomic"
    "time"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/security"
)

type RouteRegistrar func(kernelInstance kernelcontract.Kernel)

type Application struct {
    booted bool

    booting             bool
    ctx                 context.Context
    configuration       configcontract.Configuration
    runtimeFlags        *RuntimeFlags
    kernel              kernelcontract.Kernel
    embeddedPublicFiles fs.FS
    modules             []applicationcontract.Module

    registeredModuleInstances map[applicationcontract.Module]struct{}
    cliCommands           []clicontract.Command
    httpRouteRegistrars   []RouteRegistrar
    httpMiddlewares       *HttpMiddleware
    httpHandlerDecorators []applicationcontract.HttpHandlerDecorator
    httpShutdownHooks     []func()
    securityConfiguration *security.CompiledConfiguration
    routeRegistry         httpcontract.RouteRegistry
    moduleConfigurations  map[string]any
    bootCollisions        []bootCollision
    unappliedSecretMarks  []string

    unboundedDefaultCacheBackend bool

    defaultInMemorySessionStorage bool

    closePerformerClaimed atomic.Bool

    closeDoneOnce sync.Once
    closeDone     chan struct{}
}

func (instance *Application) Boot() kernelcontract.Kernel {
    if true == instance.booted {
        return instance.kernel
    }

    instance.booting = true

    defer instance.logOnRecoverAndExit()

    configuration := instance.configuration

    instance.bootModulesPreConfigurationResolve()

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

    instance.armRouteCollisionRecorder()

    instance.bootHttp()

    instance.bootModulesPostConfigurationResolve()

    instance.bootContainer()

    warnIgnoredProcessEnvironment(instance.bootLogger(), configuration, os.Environ())

    instance.bootCli()

    instance.panicOnBootCollisions()

    instance.disarmRouteCollisionRecorder()

    instance.registerKernelHttpListeners()

    instance.buildMessageBusTransportsCloser()

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

/* RegisterSecretParameter declares a parameter holding a credential. It is registered and resolved like any other; the marking only keeps it, and every parameter whose template reads it, out of the rendered configuration. */
func (instance *Application) RegisterSecretParameter(
    name string,
    value any,
) {
    instance.registerParameter(name, value, true)
}

/* MarkParameterSecret marks an existing parameter as secret. Missing names are retried before resolution and at the end of boot, then warned about if still absent. */
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

/* Configuration exposes the loaded configuration before boot so wiring code (module construction in the composition root) can read parameters — including the values melody auto-registers from the .env files — without reaching for os.Getenv. Services resolved from the container should instead read config through the resolver. */
func (instance *Application) Configuration() configcontract.Configuration {
    return instance.configuration
}

/* ProcessRole returns the resolved web, worker or all role. An explicit --role overrides the configured value, which defaults to all. Wiring decides which runners to register. Run does not join background goroutines; handlers must finish draining before returning. */
func (instance *Application) ProcessRole() string {
    return instance.runtimeFlags.Role()
}

func (instance *Application) Run() {
    _ = instance.Boot()

    markConfigurationServing(instance.configuration)

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            instance.closeAndExitOnFailure()

            return
        }

        logging.LogOnRecoverAndExitAfter(instance.resolveExitLogger(), recoveredValue, 1, instance.teardownTimeout(), instance.closeBeforeExit)
    }()

    if config.ModeCli == instance.runtimeFlags.Mode() {
        stripRuntimeFlagsFromOsArgs()

        runCliErr := instance.runCli()
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

    runHttpErr := instance.runHttp(instance.ctx)
    if nil != runHttpErr {
        exception.Panic(
            exception.FromError(runHttpErr),
        )
    }
}

func resolveCliExitError(runCliErr error) (*exception.ExitError, bool) {
    var exitError *exception.ExitError

    if false == errors.As(runCliErr, &exitError) {
        return nil, false
    }

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

    logging.LogOnRecoverAndExitAfter(instance.resolveExitLogger(), recoveredValue, 1, instance.teardownTimeout(), instance.closeBeforeExit)
}

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
        clock.NewSystemClock(),
        false,
    )
}

var applicationExit = os.Exit

var shieldedCloseStep = logging.RunShieldedStepWithin

func (instance *Application) closeAndExitOnFailure() {
    var closeErr error

    completed := shieldedCloseStep(instance.teardownTimeout(), "closing the application", func(closeContext context.Context) {
        closeErr = instance.close(closeContext)
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

func (instance *Application) teardownTimeout() (teardownTimeout time.Duration) {
    teardownTimeout = config.DefaultTeardownTimeout

    defer func() {
        if nil != recover() {
            teardownTimeout = config.DefaultTeardownTimeout
        }
    }()

    if true == internal.IsNilInterface(instance.configuration) {
        return config.DefaultTeardownTimeout
    }

    parameter := instance.configuration.Get(config.KernelTeardownTimeout)
    if true == internal.IsNilInterface(parameter) {
        return config.DefaultTeardownTimeout
    }

    teardownTimeout, teardownTimeoutErr := parameter.Duration()
    if nil != teardownTimeoutErr || 0 > teardownTimeout {
        return config.DefaultTeardownTimeout
    }

    return teardownTimeout
}

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
