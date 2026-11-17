package application

import (
    "sort"
    "strings"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* ignoredProcessEnvironmentBootWarnings answers one boot warning per process environment variable named like a resolved configuration parameter or a retired key: melody reads configuration only from the .env artifacts, so such a variable is inert. A retired name is warned with its successor whatever its value; any other variable whose value equals the resolved one is skipped, and values are never written, since they may be secrets. */
func ignoredProcessEnvironmentBootWarnings(
    configuration configcontract.Configuration,
    processEnvironment []string,
) []internal.BootWarning {
    if nil == configuration {
        return nil
    }

    declarer, _ := configuration.(configcontract.EnvironmentKeyDeclarer)

    ignoredNames := make([]string, 0)
    successors := make(map[string]string)

    for _, entry := range processEnvironment {
        name, value, found := strings.Cut(entry, "=")
        if false == found || "" == name {
            continue
        }

        if nil != declarer {
            if successor, isRetired := declarer.RetiredEnvironmentKeySuccessor(name); true == isRetired {
                ignoredNames = append(ignoredNames, name)
                successors[name] = successor

                continue
            }
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
        context := loggingcontract.Context{
            "environmentVariable": name,
        }

        if successor, isRetired := successors[name]; true == isRetired {
            context["successor"] = successor
        }

        bootWarnings = append(bootWarnings, internal.BootWarning{
            Name:    internal.BootWarningNameProcessEnvironmentVariableIgnored,
            Message: "process environment variable is ignored: melody reads configuration only from .env files",
            Context: context,
        })
    }

    return bootWarnings
}
