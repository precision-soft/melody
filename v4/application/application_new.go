package application

import (
    "context"
    "io/fs"
    "os"
    "path/filepath"
    "strings"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/kernel"
    "github.com/precision-soft/melody/v4/logging"
)

/* ApplicationConfig is what an application is built from. A field left out takes its default: no embedded env files, no embedded public files, and the framework defaults of the six limits, a 15s read timeout, a 5s read header timeout, a 30s write timeout, a 60s idle timeout, a 1 MiB header cap and a 10s teardown budget. Each limit is the application's own default for the configuration key of the same name, MELODY_HTTP_READ_TIMEOUT, MELODY_HTTP_READ_HEADER_TIMEOUT, MELODY_HTTP_WRITE_TIMEOUT, MELODY_HTTP_IDLE_TIMEOUT, MELODY_HTTP_MAX_HEADER_BYTES and MELODY_TEARDOWN_TIMEOUT: a key the deployment sets wins over the field. */
type ApplicationConfig struct {
    /* EnvFiles is the embedded filesystem the .env artifacts are read from under melody_env_embedded. */
    EnvFiles fs.FS
    /* PublicFiles is the embedded filesystem the file server serves under melody_static_embedded. */
    PublicFiles fs.FS
    /* ReadTimeout bounds reading one whole request, headers and body: at least 1ms, unlimited refused. */
    ReadTimeout bound.Duration
    /* ReadHeaderTimeout bounds reading the request headers: at least 1ms, unlimited refused. */
    ReadHeaderTimeout bound.Duration
    /* WriteTimeout bounds writing one response, armed from the request line: at least 1ms, unlimited refused. */
    WriteTimeout bound.Duration
    /* IdleTimeout bounds a keep-alive connection waiting for its next request: at least 1ms, unlimited refused. */
    IdleTimeout bound.Duration
    /* MaxHeaderBytes caps the request line and headers: at least 4 KiB, unlimited refused. */
    MaxHeaderBytes bound.Count
    /* TeardownTimeout budgets the whole ordered teardown of a clean stop: at least 1ms, unlimited admitted for a supervisor that grants no deadline. */
    TeardownTimeout bound.Duration
}

func NewApplication(
    ctx context.Context,
    applicationConfig ApplicationConfig,
) *Application {
    /* NewApplication is the outermost frame an application error can reach, with nothing above it to log or answer for the failure, so it owns the process boundary and takes the exit itself; logging.LogOnRecover does not exit */
    defer func() {
        logging.LogOnRecoverAndExit(logging.EmergencyLogger(), recover(), 1)
    }()

    if nil == ctx {
        ctx = context.Background()
    }

    embeddedEnvFiles := applicationConfig.EnvFiles
    embeddedPublicFiles := applicationConfig.PublicFiles

    projectDirectory, projectDirectoryErr := computeProjectDirectory()
    if nil != projectDirectoryErr {
        exception.Panic(exception.NewError("failed to compute project directory", nil, projectDirectoryErr))
    }

    environmentSource := newEnvironmentSource(
        projectDirectory,
        embeddedEnvFiles,
    )

    environment, newEnvironmentErr := config.NewEnvironment(environmentSource)
    if nil != newEnvironmentErr {
        exception.Panic(
            exception.NewError("failed to create environment", nil, newEnvironmentErr),
        )
    }

    configuration, newConfigurationErr := config.NewConfiguration(environment, projectDirectory)
    if nil != newConfigurationErr {
        /* the wrap names construction, not resolution: NewConfiguration also fails on validation and on building its views, and a resolve failure already carries its own message */
        exception.Panic(
            exception.NewError("could not initialize the configuration", nil, newConfigurationErr),
        )
    }

    if _, serverLimitsErr := resolveHttpServerLimits(configuration, applicationConfig); nil != serverLimitsErr {
        exception.Panic(exception.NewError("the http server limits are refused", nil, serverLimitsErr))
    }

    if _, teardownTimeoutErr := resolveTeardownTimeout(configuration, applicationConfig); nil != teardownTimeoutErr {
        exception.Panic(exception.NewError("the teardown timeout is refused", nil, teardownTimeoutErr))
    }

    routeRegistry := http.NewRouteRegistry()
    httpRouter := http.NewRouterWithRouteRegistry(routeRegistry)

    clockInstance := clock.NewSystemClock()

    kernelInstance := kernel.NewKernel(
        configuration,
        container.NewContainer(),
        httpRouter,
        event.NewEventDispatcher(clockInstance),
        clockInstance,
    )

    httpMiddleware := NewHttpMiddleware(
        newStaticFileServerOptions(embeddedPublicFiles, configuration),
        configuration,
    )

    application := &Application{
        ctx:                  ctx,
        applicationConfig:    applicationConfig,
        configuration:        configuration,
        runtimeFlags:         ParseRuntimeFlags(RuntimeFlagsConfig{Mode: configuration.Kernel().DefaultMode(), Role: configuration.Kernel().ProcessRole()}),
        kernel:               kernelInstance,
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
        ".env." + config.EnvDevelopment,
        ".env." + config.EnvDevelopment + ".local",
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
