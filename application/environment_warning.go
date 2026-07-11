package application

import (
    "sort"
    "strings"

    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* bootLogger resolves the container logger once the container phase has run, falling back to the emergency logger so boot-time diagnostics are never lost. */
func (instance *Application) bootLogger() loggingcontract.Logger {
    logger, loggerErr := logging.LoggerFromContainer(instance.kernel.ServiceContainer())
    if nil != loggerErr || nil == logger {
        return logging.EmergencyLogger()
    }

    return logger
}

/* warnIgnoredProcessEnvironment reports every process environment variable whose name matches a known configuration parameter: melody deliberately reads configuration only from the .env artifacts and never from the process environment, so such a variable is inert — a real deployment foot-gun when docker-compose sets, say, MELODY_PROCESS_ROLE expecting it to be read. The check is log-only; the known set is exactly the resolved parameter names (built-in keys, their aliases and every .env key), so unrelated variables like PATH or HOME can never match. A variable whose value equals the resolved parameter value is skipped — deployment platforms often mirror the .env values into the environment — and values are never logged, since they may be secrets. */
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
