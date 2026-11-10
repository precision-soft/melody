package application

import (
    "sort"
    "strings"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* bootLogger resolves the container logger once the container phase has run, falling back to the emergency logger so boot-time diagnostics are never lost. */
func (instance *Application) bootLogger() loggingcontract.Logger {
    logger, loggerErr := logging.LoggerFromContainer(instance.ctx, instance.kernel.ServiceContainer())
    if nil != loggerErr || nil == logger {
        return logging.EmergencyLogger()
    }

    return logger
}

/* ignoredProcessEnvironmentBootWarnings answers one boot warning per process environment variable named like a resolved configuration parameter: melody reads configuration only from the .env artifacts, so such a variable is inert. A variable whose value equals the resolved one is skipped, and values are never written, since they may be secrets. */
func ignoredProcessEnvironmentBootWarnings(
    configuration configcontract.Configuration,
    processEnvironment []string,
) []internal.BootWarning {
    if nil == configuration {
        return nil
    }

    ignoredNames := make([]string, 0)

    for _, entry := range processEnvironment {
        name, value, found := strings.Cut(entry, "=")
        if false == found || "" == name {
            continue
        }

        parameter := configuration.Get(name)
        if nil == parameter {
            continue
        }

        if value == parameter.String() {
            continue
        }

        ignoredNames = append(ignoredNames, name)
    }

    sort.Strings(ignoredNames)

    bootWarnings := make([]internal.BootWarning, 0, len(ignoredNames))
    for _, name := range ignoredNames {
        bootWarnings = append(bootWarnings, internal.BootWarning{
            Name:    internal.BootWarningNameProcessEnvironmentVariableIgnored,
            Message: "process environment variable is ignored: melody reads configuration only from .env files",
            Context: loggingcontract.Context{
                "environmentVariable": name,
            },
        })
    }

    return bootWarnings
}
