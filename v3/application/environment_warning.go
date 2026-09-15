package application

import (
    "sort"
    "strings"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func (instance *Application) bootLogger() loggingcontract.Logger {
    logger, loggerErr := logging.LoggerFromContainer(instance.kernel.ServiceContainer())
    if nil != loggerErr || nil == logger {
        return logging.EmergencyLogger()
    }

    return logger
}

func warnIgnoredProcessEnvironment(
    logger loggingcontract.Logger,
    configuration configcontract.Configuration,
    processEnvironment []string,
) {
    if nil == logger || nil == configuration {
        return
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

    if 0 == len(ignoredNames) {
        return
    }

    sort.Strings(ignoredNames)

    for _, name := range ignoredNames {
        logger.Warning(
            "process environment variable is ignored: melody reads configuration only from .env files",
            loggingcontract.Context{
                "environmentVariable": name,
            },
        )
    }
}
