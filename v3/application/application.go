package application

import (
    "context"
    "io/fs"
    "os"
    "sync"
    "time"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/openapi"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/security"
)

type RouteRegistrar func(kernelInstance kernelcontract.Kernel)

type Application struct {
    booted bool
    /* raised for the boot window so the module doors refuse a registration from inside a module boot hook: the phase loops iterate a snapshot, so such a module would receive only the hooks of the phases not yet run */
    booting             bool
    ctx                 context.Context
    configuration       configcontract.Configuration
    runtimeFlags        *RuntimeFlags
    kernel              kernelcontract.Kernel
    embeddedPublicFiles fs.FS
    modules             []applicationcontract.Module
    /* the identity set behind the module dedup: one instance reached through two providers boots once. Populated lazily, since tests assemble a bare Application without the constructor. */
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
    /* set when the framework supplied the cache backend, which keeps every entry it is given; runHttp turns it into a warning */
    unboundedDefaultCacheBackend bool
    /* set when the framework supplied the session storage; with an unbounded session ttl it grows from the request path */
    defaultInMemorySessionStorage bool
    /* runs the one close that performs the teardown and holds every sibling until it has finished: the container's own closedness cannot answer "was it me", since two concurrent closes both probe it open. The zero value works, which an Application built by literal needs. */
    closePerformerOnce sync.Once
}

func (instance *Application) Boot() kernelcontract.Kernel {
    if true == instance.booted {
        return instance.kernel
    }

    instance.booting = true

    defer instance.logOnRecoverAndExit()

    configuration := instance.configuration

    instance.bootModulesPreConfigurationResolve()

    /* the retry runs before the resolve: the marking must be on the parameter when the templates that read it resolve */
    instance.applyUnappliedSecretMarks()

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        /* the project directory is named in the failure: melody derives it from the executable location, the working directory under go run, so a binary run from elsewhere fails here with an otherwise unsuggestive "undefined environment key" */
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

    /* armed before the first route registers and disarmed after the aggregated report had its chance to raise, so every duplicate route lands in that report */
    instance.armRouteCollisionRecorder()

    /* the application's own routes register before any module's: the router breaks a dispatch tie on registration order, and the composition root wrote its route against the application */
    instance.bootHttp()

    instance.bootModulesPostConfigurationResolve()

    instance.bootContainer()

    warnIgnoredProcessEnvironment(instance.bootLogger(), configuration, os.Environ())

    instance.bootCli()

    instance.panicOnBootCollisions()

    instance.disarmRouteCollisionRecorder()

    instance.registerKernelHttpListeners()

    /* after the collisions are reported and every registration is in: an optional feature built for its teardown, not for a caller */
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

/* MarkParameterSecret marks a parameter that already exists, typically one melody registered from the .env artifacts, as holding a credential. A name that matches nothing does not fail the boot, since an environment key may be undefined in some environments; it is retried before the configuration resolves and at the end of the boot, and warned about then. */
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

/* applyUnappliedSecretMarks retries the markings that matched nothing when declared. It runs after every module registered its parameters and before the configuration resolves, so a retried marking propagates into the templates that read the secret; what still matches nothing stays queued. */
func (instance *Application) applyUnappliedSecretMarks() {
    remaining := make([]string, 0, len(instance.unappliedSecretMarks))

    for _, name := range instance.unappliedSecretMarks {
        if false == instance.configuration.MarkSecret(name) {
            remaining = append(remaining, name)
        }
    }

    instance.unappliedSecretMarks = remaining
}

/* warnUnappliedSecretMarks runs when every phase that can register a parameter has finished: a marking that still matches nothing is a misspelled name or a key undefined in this environment. */
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

    /* a duplicate is recorded for the aggregated boot report; the first registration wins until the report ends the boot */
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

/* Configuration exposes the loaded configuration before boot, so wiring code in the composition root can read parameters, the ones melody registers from the .env files included, without os.Getenv. A service resolved from the container reads config through the resolver. */
func (instance *Application) Configuration() configcontract.Configuration {
    return instance.configuration
}

/* ProcessRole is the resolved process role (config.RoleWeb, config.RoleWorker or config.RoleAll): an explicit --role flag wins over the MELODY_PROCESS_ROLE parameter, which defaults to all. Melody gates nothing on it; wiring code queries it to decide whether to register background runners on this process, and services resolve it through ServiceProcessRole. Nothing waits for those runners: when Run returns the container closes, so a runner that must finish observes the run context and drains before its handler returns. */
func (instance *Application) ProcessRole() string {
    return instance.runtimeFlags.Role()
}

func (instance *Application) Run() {
    _ = instance.Boot()

    /* from here the wiring is done and a late Resolve is an error instead of a silent rewrite under readers that already read their values */
    markConfigurationServing(instance.configuration)
    markOpenApiRegistryServing(instance.kernel.ServiceContainer())

    /* one handler owns both the teardown and the exit: os.Exit runs no defer, and a Close deferred above it would close the logger the final record is written through. The record is written first, then the teardown runs, then the exit. */
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

/* resolveCliExitError answers the exit error a failed cli run ends the process with, or nil when the failure carries none. The cause chain is walked, since the cli action folds a command's error together with the shutdown-close failures. */
func resolveCliExitError(runCliErr error) (*exception.ExitError, bool) {
    /* a typed-nil link is answered as absent, so the run falls through to the ordinary panic that carries its real error */
    return internal.ExitErrorInChain(runCliErr)
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

    /* the registry is read under exactly one name, the logging configuration; any other name could never be consulted, so it is refused rather than stored as a configuration the operator believes active */
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
        /* recorded for the aggregated boot report; the first registration wins until the report ends the boot */
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

    /* the teardown hook mirrors the one Run passes, so a boot that dies after the container was built closes it before os.Exit: the record is written first, then the close runs, then the exit. A boot that died before the kernel existed costs nothing through the nil-kernel check close starts with. */
    logging.LogOnRecoverAndExitAfter(instance.resolveExitLogger(), recoveredValue, 1, instance.teardownTimeout(), instance.closeBeforeExit)
}

/* resolveExitLogger picks the logger the final fatal record is written through: the container logger while it still writes, else a last-resort logger on the configured destination, else the emergency logger. Liveness is asked of the logger itself, since the container keeps serving built instances after Close and a closed file logger drops every write. An Application assembled without NewApplication answers the emergency logger. */
func (instance *Application) resolveExitLogger() loggingcontract.Logger {
    logger := logging.EmergencyLogger()

    /* read through the interface: this runs as an argument, before the exit handler's shield begins, so a nil receiver here would replace the reported panic with a bare traceback */
    if true == internal.IsNilInterface(instance.kernel) {
        return instance.exitFileLogger(logger)
    }

    containerLogger, loggerErr := logging.LoggerFromContainer(instance.kernel.ServiceContainer())
    if nil != loggerErr || true == internal.IsNilInterface(containerLogger) {
        /* the typed-nil clause keeps the Closed probe below off a nil receiver in the one handler that must not panic */
        return instance.exitFileLogger(logger)
    }

    closedChecker, isChecker := containerLogger.(interface{ Closed() bool })
    if true == isChecker && true == closedChecker.Closed() {
        return instance.exitFileLogger(logger)
    }

    return containerLogger
}

/* exitFileLogger builds the last-resort logger for a process dying without a live container logger, on the destination the configuration names, resolved and created as the container provider does. It runs as an argument to the exit handler, before the shield begins, so every failure answers the emergency logger instead of raising. The descriptor is surrendered to os.Exit. */
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

/* applicationExit terminates the process when the teardown of a normally-returning Run reports a failure; tests replace it to observe the exit code without stopping the test binary, the way signalContextExit is replaced */
var applicationExit = os.Exit

/* shieldedCloseStep is the door the clean-shutdown teardown runs through; tests replace it to drive the abandoned branch without waiting out the real budget, the way they replace applicationExit to observe an exit code */
var shieldedCloseStep = logging.RunShieldedStepWithin

/* closeAndExitOnFailure is Run's non-panic return: a teardown failure this call discovered becomes a non-zero exit. The teardown runs under the exit-step shield, so one Close that never returns cannot park the process; an abandoned teardown exits non-zero, and the error the step was writing is not read, since the step still runs on its own goroutine. */
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

/* teardownTimeout answers the budget the clean shutdown's teardown runs under, from the operator's parameter or the configured default. It is evaluated as an argument to the exit handler, before the shield begins, so every failure of the read, a panic included, answers the default rather than raising. A negative value is refused at boot; zero means no deadline. */
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

    /* zero is returned as it stands: it is the operator asking for no deadline */
    return teardownTimeout
}

/* refuseHttpBootWithoutEnvironment fails the boot of an http process whose .env artifacts contributed no keys: every built-in parameter has a development default, so such a binary would serve in the dev environment with debug tooling on. A cli process stays permissive, since development commands run without an environment file. */
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

/* environmentKeyCounter is the part of a configuration that can say how many keys the .env artifacts contributed; a configuration without it keeps the permissive behaviour. */
type environmentKeyCounter interface {
    EnvironmentKeyCount() int
}

/* servingMarker is the part of a configuration that can be told the wiring phase is over; it is asked for rather than demanded, so a configuration double need not carry it. */
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

/* markOpenApiRegistryServing tells the openapi registry the wiring phase is over, so a later Describe is refused at its door instead of writing under the spec handler's readers. The registry is looked up by its published name, which runs its provider in every mode; a registry without the marker, or none, is left alone, and a failing provider leaves its failure to the first consumer. */
func markOpenApiRegistryServing(serviceContainer containercontract.Container) {
    if false == serviceContainer.Has(openapi.ServiceOpenApiRegistry) {
        return
    }

    registry, getErr := serviceContainer.Get(openapi.ServiceOpenApiRegistry)
    if nil != getErr {
        return
    }

    marker, isMarker := registry.(servingMarker)
    if false == isMarker {
        return
    }

    marker.MarkServing()
}

var _ applicationcontract.ParameterRegistrar = (*Application)(nil)
var _ applicationcontract.ConfigRegistrar = (*Application)(nil)
