package application

import (
    "context"
    "errors"
    "io/fs"
    "os"

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
    /* set when the framework had to supply the cache backend itself, because the one it supplies keeps every entry it is ever given; runHttp is what turns it into a warning, and only there */
    unboundedDefaultCacheBackend bool
    /* set when the framework had to supply the session storage itself; paired with an unbounded session ttl it is the same unbounded growth, reached from the request path rather than from anything the application wrote */
    defaultInMemorySessionStorage bool
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

    instance.refuseHttpBootWithoutEnvironment()

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

/* ProcessRole is the resolved process role (config.RoleWeb, config.RoleWorker or config.RoleAll): an explicit --role flag wins over the MELODY_PROCESS_ROLE parameter, which defaults to all. Melody gates nothing on it — wiring code queries it to decide whether to register background runners (outbox relays, consumers) on this process; services resolve the same value through ServiceProcessRole.

Nothing in this major waits for those runners: when Run returns, the container closes immediately, so a goroutine still draining loses its services under it. A runner that must finish its work observes the run context and completes its drain before the handler that received the context returns. */
func (instance *Application) ProcessRole() string {
    return instance.runtimeFlags.Role()
}

func (instance *Application) Run(ctx context.Context) {
    _ = instance.Boot()

    /* boot is the last moment a parameter can still change anything: from here the wiring is done and the process is serving requests or executing a command, both against services that already read what they needed. Telling the configuration so is what turns a late Resolve into an error instead of a silent rewrite under those readers. */
    markConfigurationServing(instance.configuration)

    /* one handler owns both the teardown and the exit, because neither of two separate defers can be ordered correctly: the exit helper ends in os.Exit, so a Close deferred below it would never run, and a Close deferred above it runs first and closes the very logger the final record is written through — a file-backed logger dropped every later write and the record of the dying error survived only as a one-line stderr echo. The record is therefore written first, through a logger the teardown has not touched, and the teardown runs between the record and the exit. */
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

/* resolveCliExitError answers the exit error a failed cli run should end the process with, or nil when the failure carries none. The exit-coded error may arrive wrapped — the cli action folds a command's error together with shutdown-close failures — so the cause chain is walked rather than the top type asserted, or an intended exit code degrades into a panic with a different code. The branch is a function so the typed-nil decision can be handed a chain rather than reached through a process exit. */
func resolveCliExitError(runCliErr error) (*exception.ExitError, bool) {
    var exitError *exception.ExitError

    if false == errors.As(runCliErr, &exitError) {
        return nil, false
    }

    /* errors.As matches this type on a typed-nil link and reports success. Answering that as an exit would hand Exit a nil it refuses, and the run's real error would be discarded in favour of a message about melody's own plumbing, so the run falls through to the ordinary panic that carries it. The answer is a separate boolean because the typed nil and the absence are the same pointer, and only the boolean can tell the caller them apart. */
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

    /* the registry is consumed in exactly one place in this major, under exactly one name: the logging configuration. Any other name is unreadable by construction — no accessor exists through which a module could get it back — so accepting it would store a configuration the operator believes is active while nothing can ever consult it; a misspelling of the one supported name is the ordinary way that happens. */
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

    /* the teardown hook mirrors the one Run passes: a boot that dies after the container was built — the logger service holds the log file open from that moment — used to exit with the container never closed, because os.Exit runs no defer. The record is written first, through a logger the teardown has not touched, and the close runs between the record and the exit; Close asks IsClosed before acting, so a container that never got built costs nothing. */
    logging.LogOnRecoverAndExitAfter(instance.resolveExitLogger(), recoveredValue, 1, instance.Close)
}

/* resolveExitLogger picks the logger the final fatal record is written through: the configured container logger while it still writes, a last-resort logger opened on the configured destination when the container cannot answer — a boot that died before the logger service existed, a teardown that already closed it — and the emergency logger when even that destination is unusable. The container keeps serving built instances after Close, so liveness has to be asked of the logger itself — a file-backed logger a teardown already closed silently drops every write, and preferring it would lose the one record that explains the exit. The kernel guard covers an Application assembled without NewApplication: the one handler that must not panic answers with the emergency logger instead of dereferencing nil. */
func (instance *Application) resolveExitLogger() loggingcontract.Logger {
    logger := logging.EmergencyLogger()

    /* read through the interface, like the logger clause below: this resolver is evaluated as an argument, so it runs before the exit handler's own per-step shield begins, and a nil receiver here would replace the panic being reported with a bare traceback that runs neither the teardown nor os.Exit */
    if true == internal.IsNilInterface(instance.kernel) {
        return instance.exitFileLogger(logger)
    }

    containerLogger, loggerErr := logging.LoggerFromContainer(instance.kernel.ServiceContainer())
    if nil != loggerErr || nil == containerLogger || true == internal.IsNilInterface(containerLogger) {
        /* the typed-nil clause is latent defense: the container refuses a provider-returned or overridden typed nil with an error today, but one that did slip through a future resolution path would pass the plain comparison and answer the Closed probe below with a nil receiver — a panic in the one handler that must not panic, in place of the emergency fallback this resolver exists to provide */
        return instance.exitFileLogger(logger)
    }

    closedChecker, isChecker := containerLogger.(interface{ Closed() bool })
    if true == isChecker && true == closedChecker.Closed() {
        return instance.exitFileLogger(logger)
    }

    return containerLogger
}

/* exitFileLogger builds the last-resort logger for a process that is dying without a live container logger, on the destination the configuration names, so a deployment that reads var/log sees why the process ended instead of a boot failure that left no trace outside stderr. It is best-effort by construction: it runs as an argument to the exit handler, before the per-step shield begins, so any failure on the way — a configuration never built, an unopenable path, a module configuration that panics — answers the emergency logger instead of raising. The kernel view is built with the configuration itself, so the destination is readable for every failure after construction, resolved and created the way the container provider resolves and creates it. The descriptor is deliberately surrendered to os.Exit: the process ends before anything could close it. */
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

/* applicationExit terminates the process when the teardown of a normally-returning Run reports a failure; tests replace it to observe the exit code without stopping the test binary, the way signalContextExit is replaced */
var applicationExit = os.Exit

/* closeAndExitOnFailure is Run's non-panic return: a teardown failure this call itself discovered turns into a non-zero exit, so a supervisor sees a shutdown that lost something — a failed flush, a close that errored — instead of recording a clean exit 0 whose only trace was one stderr line. A close somebody else already performed reported its failure through its own channel and keeps its own exit code. */
func (instance *Application) closeAndExitOnFailure() {
    closeErr := instance.close()
    if nil == closeErr {
        return
    }

    applicationExit(1)
}

/* refuseHttpBootWithoutEnvironment fails the boot of an http process whose .env artifacts contributed no keys at all. Every built-in parameter has a development default — environment dev, debug tooling, debug commands, debug log level — so a production binary run from a directory without its .env files would otherwise serve on all of them, announced by nothing louder than one warning; refusing is the same direction the empty CORS allow list took, because booting the wrong environment is the widening. A cli process stays permissive: development commands legitimately run without any environment file, and a command takes its configuration with it when it exits. */
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

/* environmentKeyCounter is the part of a configuration that can say how many keys the .env artifacts contributed. Asked for rather than demanded, the way servingMarker is: only the http boot refusal reads it, and a configuration double that does not carry it simply keeps the permissive behavior. */
type environmentKeyCounter interface {
    EnvironmentKeyCount() int
}

/* servingMarker is the part of a configuration that can be told the wiring phase is over. It is asked for rather than demanded: only the application emits the signal, so requiring every configcontract.Configuration — every test double included — to carry the method would cost more than it buys. */
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
