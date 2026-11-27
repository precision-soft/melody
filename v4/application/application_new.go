package application

import (
    "context"
    "io/fs"
    "os"
    "path/filepath"
    "strings"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/cli"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/http/middleware"
    "github.com/precision-soft/melody/v4/internal/kernelseam"
    "github.com/precision-soft/melody/v4/kernel"
    "github.com/precision-soft/melody/v4/logging"
    "github.com/precision-soft/melody/v4/messagebus"
)

/* ApplicationConfig is what an application is built from. A field left out takes its default: no embedded env files, no embedded public files, the framework's http server and http kernel, and the framework defaults of the limits, a 10s teardown budget and 1,024 container creation waiters. Each limit is the application's own default for the configuration key of the same name, MELODY_TEARDOWN_TIMEOUT and MELODY_CONTAINER_CREATION_WAITERS: the field is the default of its kernel.* parameter, which reads that key unless ParameterOverrides rewrites the parameter. */
type ApplicationConfig struct {
    /* Version is the application's own version; empty is undeclared, <unknown> in a table and --version, "" in json. */
    Version string
    /* EnvironmentFiles is the embedded filesystem the .env artifacts are read from under melody_env_embedded. */
    EnvironmentFiles fs.FS
    /* PublicFiles is the embedded filesystem the file server serves under melody_static_embedded. */
    PublicFiles fs.FS
    /* HttpPublicUrl is the public base an absolute url is generated behind, the default of MELODY_HTTP_PUBLIC_URL, which wins when a deployment sets it: an http or https url with a host name and no user info, query, fragment or empty, "." or ".." path segment, its path joined before every route's; Http().PublicUrl() answers the key, then this field; empty is none. */
    HttpPublicUrl string
    /* HttpServer is the http server Run listens with; the zero value is the framework's. */
    HttpServer HttpServerConfig
    /* HttpKernel is what the http kernel is built with, its one source; the zero value is the framework's. UnavailableRetryAfter, SessionPolicy.EmptyOnOutage and SessionCookiePolicy.Secure are the defaults of MELODY_HTTP_UNAVAILABLE_RETRY_AFTER, MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE and MELODY_HTTP_SESSION_COOKIE_SECURE, which win when a deployment sets them; Middlewares is refused, the application's own going through RegisterHttpMiddlewares. */
    HttpKernel http.KernelConfig
    /* TeardownTimeout budgets the whole ordered teardown of a clean stop: at least 1ms, unlimited admitted for a supervisor that grants no deadline. */
    TeardownTimeout bound.Duration
    /* ScopeTeardownTimeout bounds the close of a request's, a command's, a message's and a cron run's scope: 3s when left out, at least 1ms, unlimited admitted for no term. */
    ScopeTeardownTimeout bound.Duration

    /* Namespace is the application namespace, the default of MELODY_NAMESPACE, which wins when a deployment sets it: every store family is handed it and derives its default names from it, melody when left out; 1 to 32 characters of a-z, 0-9, _ and -, beginning and ending with a letter or a digit, refused naming the field otherwise. Kernel().Namespace() answers the resolved value. */
    Namespace string

    /* ContainerCreationWaiters bounds the resolutions that may wait on one in-flight creation: 1,024 when left out, at least 1, unlimited admitted. */
    ContainerCreationWaiters bound.Count
    /* ContainerTeardownSequential closes the container's services one after the other instead of in waves, for services whose Close methods touch shared external state the container cannot see; the field is the default of kernel.container_teardown_sequential, which reads MELODY_CONTAINER_TEARDOWN_SEQUENTIAL unless overridden. */
    ContainerTeardownSequential bool
    /* ContainerTeardownWalk bounds the walk that infers the container's teardown edges, handed to ContainerConfig.TeardownWalk: 12 deep, 256 fields or elements, 65,536 items when left out, unlimited refused, refused naming the ContainerConfig field. */
    ContainerTeardownWalk container.TeardownWalkConfig
    /* ContainerCloseFailureStackBytes bounds what one failed close contributes to the teardown error, handed to ContainerConfig.CloseFailureStackBytes: 2,048 when left out, at least 4, unlimited admitted. */
    ContainerCloseFailureStackBytes bound.Count

    /* CliErrorCauseChainDepth bounds the causes a failed command's [error] line carries, handed to cli.RootConfig.ErrorCauseChainDepth: 8 when left out, at least 0, unlimited refused, refused naming the RootConfig field. */
    CliErrorCauseChainDepth bound.Count

    /* EnvironmentKeys are the .env keys the application owns, outside the MELODY_ namespace, each with the default, rule, secret mark, retired spellings and refusals of a module's key. They are declared when the application is built, before any parameter is read. A key in the MELODY_ namespace is refused. */
    EnvironmentKeys []configcontract.EnvironmentKey

    /* Events is what the application declares on the framework's own events, by event name: kernel.request, kernel.controller, kernel.response, kernel.terminate, kernel.exception, the six security.* events and http.request.profile. The application declares all twelve through EventDispatcher.DeclareEvent when it is built, each with the framework's own tags followed by the TagsToRunAfterFailure given here and under the dispatcher's rules, so a later DeclareEvent on one of those names is refused as already declared. A name that is not a framework event refuses the application by name, and a tag the dispatcher refuses is answered naming this field; an application event is declared through EventDispatcher.DeclareEvent. */
    Events map[string]eventcontract.EventConfig

    /* SecurityHeaders is the security headers every response carries, through a middleware outside the static file server and a kernel.response listener; the zero value sends the defaults, Disabled none. */
    SecurityHeaders middleware.SecurityHeadersConfig

    /* MessageBus is what the framework hands the melody:messagebus:consume command it registers. */
    MessageBus MessageBusConfig

    /* ParameterOverrides rewrites, by parameter name, the template of a framework (kernel.*) parameter or of a parameter a module's key feeds: "%env(APP_Y)%", "%app.param%", a literal, or any mix. The override is the parameter's one source: the key that would feed it is not read, and a .env artifact that still sets that key refuses the boot naming the override, unless the override itself reads it. kernel.environment and kernel.project_directory are refused. An override of a kernel.* parameter is resolved when the application is built, so it reads keys, literals and other kernel.* parameters only; a name no framework or module key feeds refuses the boot. */
    ParameterOverrides map[string]string
}

/* HttpServerConfig is the http server Run listens with. Each field is the application's default of the key named beside it, which wins when a deployment sets it; a value outside its rule refuses NewApplication naming the field. */
type HttpServerConfig struct {
    /* ReadTimeout bounds reading one whole request, headers and body, the default of MELODY_HTTP_READ_TIMEOUT: 15s when left out, at least 1ms, unlimited refused. */
    ReadTimeout bound.Duration
    /* ReadHeaderTimeout bounds reading the request headers, the default of MELODY_HTTP_READ_HEADER_TIMEOUT: 5s when left out, at least 1ms, unlimited refused. */
    ReadHeaderTimeout bound.Duration
    /* WriteTimeout bounds writing one response, armed from the request line, the default of MELODY_HTTP_WRITE_TIMEOUT: 30s when left out, at least 1ms, unlimited refused. */
    WriteTimeout bound.Duration
    /* IdleTimeout bounds a keep-alive connection waiting for its next request, the default of MELODY_HTTP_IDLE_TIMEOUT: 60s when left out, at least 1ms, unlimited refused. */
    IdleTimeout bound.Duration
    /* MaxHeaderBytes caps the request line and headers, the default of MELODY_HTTP_MAX_HEADER_BYTES: 1 MiB when left out, at least 4 KiB, unlimited refused. */
    MaxHeaderBytes bound.Count
    /* ShutdownTimeout is how long a stopping http server waits for the requests it admitted, the default of MELODY_HTTP_SHUTDOWN_TIMEOUT: 5s when left out, at least 1ms, unlimited refused. */
    ShutdownTimeout bound.Duration
}

/* MessageBusConfig is the message bus configuration the application owns; the zero value is the framework's. */
type MessageBusConfig struct {
    /* Retry is the retry policy of the consume command the framework registers, its one source; a field outside its rule refuses NewApplication by name. */
    Retry messagebus.RetryPolicy
}

func NewApplication(
    ctx context.Context,
    applicationConfig ApplicationConfig,
) *Application {
    /* NewApplication is the outermost frame an application error can reach, with nothing above it to log or answer for the failure, so it owns the process boundary and takes the exit itself; logging.LogOnRecover does not exit */
    defer func() {
        logging.LogOnRecoverAndExit(logging.EmergencyLogger(), recover(), 1, logging.ExitConfig{})
    }()

    if nil == ctx {
        ctx = context.Background()
    }

    embeddedEnvironmentFiles := applicationConfig.EnvironmentFiles
    embeddedPublicFiles := applicationConfig.PublicFiles

    projectDirectory, projectDirectoryErr := computeProjectDirectory()
    if nil != projectDirectoryErr {
        exception.Panic(exception.NewError("failed to compute project directory", nil, projectDirectoryErr))
    }

    environmentSource := newEnvironmentSource(
        projectDirectory,
        embeddedEnvironmentFiles,
    )

    environment, newEnvironmentErr := config.NewEnvironment(ctx, environmentSource)
    if nil != newEnvironmentErr {
        exception.Panic(
            exception.NewError("failed to create environment", nil, newEnvironmentErr),
        )
    }

    configuration, newConfigurationErr := config.NewConfiguration(environment, projectDirectory, config.ConfigurationConfig{EnvironmentKeys: applicationConfig.EnvironmentKeys, ParameterOverrides: applicationConfig.ParameterOverrides, HttpPublicUrl: applicationConfig.HttpPublicUrl, Namespace: applicationConfig.Namespace})
    if nil != newConfigurationErr {
        /* the wrap names construction, not resolution: NewConfiguration also fails on validation and on building its views, and a resolve failure already carries its own message */
        exception.Panic(
            exception.NewError("could not initialize the configuration", nil, newConfigurationErr),
        )
    }

    if _, serverLimitsErr := resolveHttpServerLimits(configuration, applicationConfig); nil != serverLimitsErr {
        exception.Panic(exception.NewError("the http server limits are refused", nil, serverLimitsErr))
    }

    if _, httpShutdownTimeoutErr := resolveHttpShutdownTimeout(configuration, applicationConfig); nil != httpShutdownTimeoutErr {
        exception.Panic(exception.NewError("the http shutdown timeout is refused", nil, httpShutdownTimeoutErr))
    }

    if _, teardownTimeoutErr := resolveTeardownTimeout(configuration, applicationConfig); nil != teardownTimeoutErr {
        exception.Panic(exception.NewError("the teardown timeout is refused", nil, teardownTimeoutErr))
    }

    scopeTeardownTimeout, scopeTeardownTimeoutErr := resolveScopeTeardownTimeout(configuration, applicationConfig)
    if nil != scopeTeardownTimeoutErr {
        exception.Panic(exception.NewError("the scope teardown timeout is refused", nil, scopeTeardownTimeoutErr))
    }

    /* the tree is built at the cli run, so its depth is refused here, with the application's other configuration errors */
    if _, cliErrorCauseChainDepthErr := bound.Resolve(applicationConfig.CliErrorCauseChainDepth, cli.ErrorCauseChainDepthRule()); nil != cliErrorCauseChainDepthErr {
        exception.Panic(exception.FromError(cliErrorCauseChainDepthErr))
    }

    containerCreationWaiters, containerCreationWaitersErr := resolveContainerCreationWaiters(configuration, applicationConfig)
    if nil != containerCreationWaitersErr {
        exception.Panic(exception.NewError("the container creation waiters are refused", nil, containerCreationWaitersErr))
    }

    unavailableRetryAfter, unavailableRetryAfterErr := resolveUnavailableRetryAfter(configuration, applicationConfig)
    if nil != unavailableRetryAfterErr {
        exception.Panic(exception.NewError("the unavailable retry after is refused", nil, unavailableRetryAfterErr))
    }

    if 0 != len(applicationConfig.HttpKernel.Middlewares) {
        exception.Panic(
            exception.NewError(
                "the application's http middlewares are registered through RegisterHttpMiddlewares",
                exceptioncontract.Context{
                    "field": "ApplicationConfig.HttpKernel.Middlewares",
                    "count": len(applicationConfig.HttpKernel.Middlewares),
                },
                nil,
            ),
        )
    }

    /* the key a deployment set wins over the field, so the kernel is built once with the value it serves */
    httpKernelConfig := applicationConfig.HttpKernel
    httpKernelConfig.UnavailableRetryAfter = bound.Of(unavailableRetryAfter)
    httpKernelConfig.SessionPolicy.EmptyOnOutage = resolveSessionEmptyOnOutage(configuration, applicationConfig)
    httpKernelConfig.SessionCookiePolicy.Secure = resolveSessionCookieSecure(configuration, applicationConfig)

    routeRegistry := http.NewRouteRegistry()
    httpRouter := http.NewRouterWithRouteRegistry(routeRegistry)

    clockInstance := clock.NewSystemClock()

    /* declared before the kernel holds the dispatcher, so nothing can declare a framework event ahead of the framework */
    eventDispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clockInstance})
    if eventsErr := declareFrameworkEvents(eventDispatcher, applicationConfig.Events); nil != eventsErr {
        exception.Panic(exception.NewError("the events are refused", nil, eventsErr))
    }

    kernelInstance := kernel.NewKernel(
        kernelseam.NewToken(),
        configuration,
        container.NewContainer(container.ContainerConfig{
            ScopeTeardownTimeout:   scopeTeardownTimeout.Limit(),
            CreationWaiters:        containerCreationWaiters.Limit(),
            TeardownSequential:     resolveContainerTeardownSequential(configuration, applicationConfig),
            TeardownWalk:           applicationConfig.ContainerTeardownWalk,
            CloseFailureStackBytes: applicationConfig.ContainerCloseFailureStackBytes,
        }),
        httpRouter,
        eventDispatcher,
        clockInstance,
    )

    httpKernel := http.NewKernel(httpRouter, httpKernelConfig)

    httpMiddleware := NewHttpMiddleware(
        newStaticFileServerConfig(embeddedPublicFiles, configuration),
    )
    /* built when disabled too, so a malformed value is refused although nothing is sent */
    securityHeaders := middleware.NewSecurityHeaders(applicationConfig.SecurityHeaders)
    if false == applicationConfig.SecurityHeaders.Disabled {
        httpMiddleware.securityHeaders = securityHeaders
    }

    /* built here, so a retry policy outside its rule refuses the application before anything boots */
    consumeCommand := messagebus.NewConsumeCommand(messagebus.ConsumeCommandConfig{Retry: applicationConfig.MessageBus.Retry})

    application := &Application{
        consumeCommand:       consumeCommand,
        ctx:                  ctx,
        applicationConfig:    applicationConfig,
        configuration:        configuration,
        runtimeFlags:         ParseRuntimeFlags(RuntimeFlagsConfig{Mode: configuration.Kernel().DefaultMode(), Role: configuration.Kernel().ProcessRole()}),
        kernel:               kernelInstance,
        httpKernel:           httpKernel,
        embeddedPublicFiles:  embeddedPublicFiles,
        modules:              make([]applicationcontract.Module, 0),
        cliCommands:          make([]clicontract.Command, 0),
        httpRouteRegistrars:  make([]RouteRegistrar, 0),
        httpMiddlewares:      httpMiddleware,
        routeRegistry:        routeRegistry,
        moduleConfigurations: make(map[string]any),
    }

    return application
}

func computeProjectDirectory() (string, error) {
    executablePath, executableErr := os.Executable()
    if nil != executableErr {
        return "",
            exception.NewError(
                "failed to get executable path",
                nil,
                executableErr,
            )
    }

    executablePath, resolveErr := resolveExecutablePath(executablePath)
    if nil != resolveErr {
        return "", resolveErr
    }

    executableDirectory := filepath.Dir(executablePath)

    if true == isGoRunExecutableDirectory(executableDirectory) {
        workingDirectory, getwdErr := os.Getwd()
        if nil != getwdErr {
            return "",
                exception.NewError(
                    "failed to get working directory",
                    nil,
                    getwdErr,
                )
        }

        if true == workingDirectoryHasEnvironmentFile(workingDirectory) {
            absoluteWorkingDirectory, filepathAbsErr := filepath.Abs(workingDirectory)
            if nil != filepathAbsErr {
                return "",
                    exception.NewError(
                        "failed to determine absolute working directory",
                        exceptioncontract.Context{
                            "workingDirectory": workingDirectory,
                        },
                        filepathAbsErr,
                    )
            }

            return absoluteWorkingDirectory, nil
        }

        projectDirectory, findProjectRootStartingFromErr := findProjectRootStartingFrom(workingDirectory)
        if nil == findProjectRootStartingFromErr {
            return projectDirectory, nil
        }

        absoluteWorkingDirectory, filepathAbsErr := filepath.Abs(workingDirectory)
        if nil != filepathAbsErr {
            return "",
                exception.NewError(
                    "failed to determine absolute working directory",
                    exceptioncontract.Context{
                        "workingDirectory": workingDirectory,
                    },
                    filepathAbsErr,
                )
        }

        return absoluteWorkingDirectory, nil
    }

    absoluteExecutableDirectory, filepathAbsErr := filepath.Abs(executableDirectory)
    if nil != filepathAbsErr {
        return "",
            exception.NewError(
                "failed to determine absolute executable directory",
                exceptioncontract.Context{
                    "executableDirectory": executableDirectory,
                },
                filepathAbsErr,
            )
    }

    return absoluteExecutableDirectory, nil
}

/* isGoRunExecutableDirectory recognizes the directories the go tool runs a build from: a path segment named go-build followed by digits only, so /opt/go-builder/bin is not one. The digits may be absent: go run caches its executable under the build cache, whose directory is named go-build alone. */
func isGoRunExecutableDirectory(executableDirectory string) bool {
    for _, segment := range strings.Split(executableDirectory, string(filepath.Separator)) {
        if false == strings.HasPrefix(segment, "go-build") {
            continue
        }

        suffix := segment[len("go-build"):]

        isNumericSuffix := true
        for _, character := range suffix {
            if character < '0' || character > '9' {
                isNumericSuffix = false
                break
            }
        }

        if true == isNumericSuffix {
            return true
        }
    }

    return false
}

/* workingDirectoryHasEnvironmentFile reports whether the directory holds any environment file the source would load: .env, .env.local, or the development pair that applies when no .env names another environment. A directory named like one is not one, and a stat error other than not-exist counts as present, since both callers act on absence. */
func workingDirectoryHasEnvironmentFile(directoryPath string) bool {
    candidates := []string{
        ".env",
        ".env.local",
        ".env." + config.EnvironmentDevelopment,
        ".env." + config.EnvironmentDevelopment + ".local",
    }

    for _, candidate := range candidates {
        fileInfo, statErr := os.Stat(filepath.Join(directoryPath, candidate))
        if nil == statErr {
            if false == fileInfo.IsDir() {
                return true
            }

            continue
        }

        if false == os.IsNotExist(statErr) {
            return true
        }
    }

    return false
}

func findProjectRootStartingFrom(startDirectory string) (string, error) {
    currentDirectory := startDirectory

    for {
        goModPath := filepath.Join(currentDirectory, "go.mod")
        fileInfo, err := os.Stat(goModPath)
        if nil == err && false == fileInfo.IsDir() {
            return currentDirectory, nil
        }

        parentDirectory := filepath.Dir(currentDirectory)
        if currentDirectory == parentDirectory {
            break
        }

        currentDirectory = parentDirectory
    }

    return "",
        exception.NewError(
            "could not locate project root starting from directory",
            exceptioncontract.Context{
                "startDirectory":       startDirectory,
                "lastCheckedDirectory": currentDirectory,
            },
            nil,
        )
}

/* resolveExecutablePath follows the symlinks of the executable's path; the refusal carries the path handed in, since the resolved one is empty on a failure. */
func resolveExecutablePath(executablePath string) (string, error) {
    resolvedExecutablePath, evalSymlinksErr := filepath.EvalSymlinks(executablePath)
    if nil != evalSymlinksErr {
        return "",
            exception.NewError(
                "failed to resolve executable path",
                exceptioncontract.Context{
                    "executablePath": executablePath,
                },
                evalSymlinksErr,
            )
    }

    return resolvedExecutablePath, nil
}
