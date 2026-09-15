package application

import (
    "os"
    "strings"

    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

type RuntimeFlags struct {
    mode string
    role string
}

func NewRuntimeFlags(mode string) *RuntimeFlags {
    return NewRuntimeFlagsWithRole(mode, config.RoleAll)
}

func NewRuntimeFlagsWithRole(mode string, role string) *RuntimeFlags {
    if "" == role {
        role = config.RoleAll
    }

    return &RuntimeFlags{
        mode: mode,
        role: role,
    }
}

func (instance *RuntimeFlags) Mode() string {
    return instance.mode
}

func (instance *RuntimeFlags) Role() string {
    return instance.role
}

func ParseRuntimeFlags(defaultMode string) *RuntimeFlags {
    return ParseRuntimeFlagsWithRole(defaultMode, config.RoleAll)
}

/* ParseRuntimeFlagsWithRole resolves mode and process role from os.Args. Explicit --mode/-mode and --role/-role override defaults and are removed before CLI parsing. Other arguments imply CLI mode; runtime flags alone do not. Foreign value-taking flags before the command require --flag=value because their arity is unknown. */
func ParseRuntimeFlagsWithRole(defaultMode string, defaultRole string) *RuntimeFlags {
    arguments := os.Args

    parsedMode, modeFlagPresent := parseRuntimeFlagFromArguments(arguments, "mode")
    parsedRole, roleFlagPresent := parseRuntimeFlagFromArguments(arguments, "role")

    mode := defaultMode
    if "" != parsedMode {
        mode = parsedMode
    } else if true == modeFlagPresent {

        mode = ""
    } else if true == hasNonRuntimeFlagArguments(arguments) {
        mode = config.ModeCli
    }

    if config.ModeHttp != mode && config.ModeCli != mode {
        exception.Panic(
            exception.NewError(
                "invalid mode: --mode is a runtime flag that must precede the cli command and accepts only http or cli; if this value came from a command's own --mode flag, place that flag after the command name",
                exceptioncontract.Context{
                    "mode": mode,
                },
                nil,
            ),
        )
    }

    role := defaultRole
    if "" != parsedRole {
        role = parsedRole
    } else if true == roleFlagPresent {

        role = ""
    } else if "" == role {
        role = config.RoleAll
    }

    if config.RoleWeb != role && config.RoleWorker != role && config.RoleAll != role {
        exception.Panic(
            exception.NewError(
                "invalid role: --role is a runtime flag that must precede the cli command and accepts only web, worker or all; if this value came from a command's own --role flag, place that flag after the command name",
                exceptioncontract.Context{
                    "role": role,
                },
                nil,
            ),
        )
    }

    return NewRuntimeFlagsWithRole(mode, role)
}

func parseRuntimeFlagFromArguments(arguments []string, flagName string) (string, bool) {
    boundary := subcommandBoundaryIndex(arguments)

    parsedValue := ""
    present := false

    for index := 1; index < boundary; index++ {
        argument := strings.TrimSpace(arguments[index])
        if "" == argument {
            continue
        }

        flagValue, matched, consumeNext := parseRuntimeFlagValue(argument, flagName)
        if false == matched {
            continue
        }

        present = true

        if true == consumeNext {

            parsedValue = ""

            if index+1 < boundary {
                nextValue := strings.TrimSpace(arguments[index+1])
                if "" != nextValue && false == strings.HasPrefix(nextValue, "-") {
                    parsedValue = nextValue
                    index++
                }
            }

            continue
        }

        parsedValue = flagValue
    }

    return parsedValue, present
}

func subcommandBoundaryIndex(arguments []string) int {
    skipNext := false

    for index := 1; index < len(arguments); index++ {
        argument := strings.TrimSpace(arguments[index])
        if "" == argument {
            continue
        }

        if true == skipNext {
            skipNext = false

            continue
        }

        if "--" == argument {
            return index
        }

        if false == strings.HasPrefix(argument, "-") {
            return index
        }

        matched, consumeNext := isRuntimeFlagArgument(argument)
        if true == matched {
            skipNext = consumeNext && runtimeFlagConsumesNextArgument(arguments, index)

            continue
        }
    }

    return len(arguments)
}

func parseRuntimeFlagValue(argument string, flagName string) (string, bool, bool) {
    if "-"+flagName == argument || "--"+flagName == argument {
        return "", true, true
    }

    if true == strings.HasPrefix(argument, "-"+flagName+"=") {
        return strings.TrimSpace(strings.TrimPrefix(argument, "-"+flagName+"=")), true, false
    }

    if true == strings.HasPrefix(argument, "--"+flagName+"=") {
        return strings.TrimSpace(strings.TrimPrefix(argument, "--"+flagName+"=")), true, false
    }

    return "", false, false
}

var runtimeFlagNames = []string{"mode", "role"}

func runtimeFlagConsumesNextArgument(arguments []string, index int) bool {
    if index+1 >= len(arguments) {
        return false
    }

    nextValue := strings.TrimSpace(arguments[index+1])

    return "" != nextValue && false == strings.HasPrefix(nextValue, "-")
}

func isRuntimeFlagArgument(argument string) (bool, bool) {
    for _, flagName := range runtimeFlagNames {
        if "-"+flagName == argument || "--"+flagName == argument {
            return true, true
        }

        if true == strings.HasPrefix(argument, "-"+flagName+"=") || true == strings.HasPrefix(argument, "--"+flagName+"=") {
            return true, false
        }
    }

    return false, false
}

func hasNonRuntimeFlagArguments(arguments []string) bool {
    if 2 > len(arguments) {
        return false
    }

    skipNext := false

    for index := 1; index < len(arguments); index++ {
        argument := strings.TrimSpace(arguments[index])
        if "" == argument {
            continue
        }

        if true == skipNext {
            skipNext = false
            continue
        }

        matched, consumeNext := isRuntimeFlagArgument(argument)
        if true == matched {
            skipNext = consumeNext && runtimeFlagConsumesNextArgument(arguments, index)
            continue
        }

        return true
    }

    return false
}

func stripRuntimeFlagsFromOsArgs() {
    originalArguments := os.Args
    if 0 == len(originalArguments) {
        return
    }

    boundary := subcommandBoundaryIndex(originalArguments)

    cleanedArguments := make([]string, 0, len(originalArguments))
    cleanedArguments = append(cleanedArguments, originalArguments[0])

    skipNext := false

    for index := 1; index < boundary; index++ {
        argument := strings.TrimSpace(originalArguments[index])
        if "" == argument {
            continue
        }

        if true == skipNext {
            skipNext = false
            continue
        }

        matched, consumeNext := isRuntimeFlagArgument(argument)
        if true == matched {
            skipNext = consumeNext && runtimeFlagConsumesNextArgument(originalArguments, index)
            continue
        }

        cleanedArguments = append(cleanedArguments, originalArguments[index])
    }

    cleanedArguments = append(cleanedArguments, originalArguments[boundary:]...)

    os.Args = cleanedArguments
}
