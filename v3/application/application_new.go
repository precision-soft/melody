package application

import (
    "context"
    "io/fs"
    "os"
    "path/filepath"
    "strings"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/event"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/kernel"
    "github.com/precision-soft/melody/v3/logging"
)

func NewApplication(
    ctx context.Context,
    embeddedEnvFiles fs.FS,
    embeddedPublicFiles fs.FS,
) *Application {
    /* NewApplication is the outermost frame an application error can reach — there is no application yet, so nothing above it can log or answer for the failure. It therefore owns the process boundary and takes the exit itself, through the helper named for it; logging.LogOnRecover deliberately does not exit, so leaving that one here would let a construction failure walk out as a bare runtime panic. */
    defer func() {
        logging.LogOnRecoverAndExit(logging.EmergencyLogger(), recover(), 1)
    }()

    if nil == ctx {
        ctx = context.Background()
    }

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
        exception.Panic(
            exception.NewError("could not resolve the config parameters", nil, newConfigurationErr),
        )
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
        configuration:        configuration,
        runtimeFlags:         ParseRuntimeFlagsWithRole(configuration.Kernel().DefaultMode(), configuration.Kernel().ProcessRole()),
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

    executablePath, evalSymlinksErr := filepath.EvalSymlinks(executablePath)
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

    executableDirectory := filepath.Dir(executablePath)

    if true == strings.Contains(executableDirectory, string(filepath.Separator)+"go-build") {
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

/* workingDirectoryHasEnvironmentFile reports whether the directory holds any environment file the source would load — .env, .env.local, or the development-environment pair that applies when no .env names another environment. A project configured solely through .env.dev boots fine without a .env, so ignoring that shape would emit a missing-.env hint that misattributes a plain unresolved key, or walk away from a working directory that is in fact the project root. A directory named like an environment file is not one. A stat error other than not-exist cannot prove the file absent, so it counts as present: both callers act on absence, and acting is the wrong move while the file may in fact be there. */
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
