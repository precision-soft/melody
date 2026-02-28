package application

import (
    "io/fs"
    "os"
    "path/filepath"
    "strings"

    applicationcontract "github.com/precision-soft/melody/application/contract"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/container"
    "github.com/precision-soft/melody/event"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/http"
    "github.com/precision-soft/melody/kernel"
    "github.com/precision-soft/melody/logging"
)

func NewApplication(
    embeddedEnvFiles fs.FS,
    embeddedPublicFiles fs.FS,
) *Application {
    defer logging.LogOnRecover(logging.EmergencyLogger(), true)

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
        configuration:       configuration,
        runtimeFlags:        ParseRuntimeFlags(configuration.Kernel().DefaultMode()),
        kernel:              kernelInstance,
        embeddedPublicFiles: embeddedPublicFiles,
        modules:             make([]applicationcontract.Module, 0),
        cliCommands:         make([]clicontract.Command, 0),
        httpRouteRegistrars: make([]RouteRegistrar, 0),
        httpMiddlewares:     httpMiddleware,
        routeRegistry:       routeRegistry,
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
