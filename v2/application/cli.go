package application

import (
    "os"
    "strings"

    "github.com/precision-soft/melody/v2/config"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
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

/* ParseRuntimeFlagsWithRole resolves the runtime mode and process role from os.Args. An explicit --mode wins, any other non-runtime argument implies cli, otherwise the configured default applies; an explicit --role wins over the configured default, which is how containers built from one image are told apart, since melody reads configuration only from .env artifacts. Both flags are runtime-only, never imply cli mode and are stripped before the cli framework parses the arguments. A value-taking flag placed before the command must use its --flag=value form (see subcommandBoundaryIndex). */
func ParseRuntimeFlagsWithRole(defaultMode string, defaultRole string) *RuntimeFlags {
    arguments := os.Args

    parsedMode, modeFlagPresent := parseRuntimeFlagFromArguments(arguments, "mode")
    parsedRole, roleFlagPresent := parseRuntimeFlagFromArguments(arguments, "role")

    mode := defaultMode
    if "" != parsedMode {
        mode = parsedMode
    } else if true == modeFlagPresent {
        /* an explicitly empty --mode fails closed at the validation below instead of booting the configured default */
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
        /* an explicitly empty --role fails closed at the validation below instead of widening to RoleAll */
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

/* parseRuntimeFlagFromArguments returns the value of a runtime flag and whether it was explicitly present, scanning only the region before the cli subcommand (see subcommandBoundaryIndex), so a --role after the command name is the command's own. The last occurrence wins, even when empty, and presence lets the caller fail closed on an empty value. */
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
            /* a bare flag with no consumable next token contributes the empty value, so an earlier occurrence does not survive */
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

/* subcommandBoundaryIndex returns the index of the first token that ends the runtime-flag region: the cli subcommand name, a bare "--", or the end of the arguments; from there on everything belongs to the command. Runtime flags and their values are skipped; any other flag belongs to the region. A foreign flag's arity is unknowable, so a space-separated value before the command reads as the command name and ends the region. */
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

/* runtimeFlagNames lists the flags owned by the runtime itself: they never imply cli mode and are stripped from os.Args before the cli framework sees them. */
var runtimeFlagNames = []string{"mode", "role"}

/* runtimeFlagConsumesNextArgument mirrors parseRuntimeFlagFromArguments: a bare "--role" takes the next token only when it could be a value, so the two agree on what belongs to the runtime flag and the command's own next flag is not deleted. */
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

    /* everything from the subcommand boundary on is kept verbatim for the cli framework */
    cleanedArguments = append(cleanedArguments, originalArguments[boundary:]...)

    os.Args = cleanedArguments
}
