package application

import (
    "os"
    "strings"

    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
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

/* ParseRuntimeFlagsWithRole resolves the runtime mode and process role from os.Args. The mode: an explicit --mode/-mode wins, any other non-runtime argument implies cli, otherwise the configured default applies. The role: an explicit --role/-role wins over the configured default — the flag exists because melody reads configuration only from .env artifacts, never from the process environment, so a docker-compose deployment differentiates containers built from one image with `command: ["/app", "--role=worker"]`. Both flags are runtime-only: they never imply cli mode and are stripped before the cli framework parses the arguments. */
func ParseRuntimeFlagsWithRole(defaultMode string, defaultRole string) *RuntimeFlags {
    arguments := os.Args

    parsedMode := parseRuntimeFlagFromArguments(arguments, "mode")
    parsedRole := parseRuntimeFlagFromArguments(arguments, "role")

    mode := defaultMode
    if "" != parsedMode {
        mode = parsedMode
    } else {
        if true == hasNonRuntimeFlagArguments(arguments) {
            mode = config.ModeCli
        }
    }

    if config.ModeHttp != mode && config.ModeCli != mode {
        exception.Panic(
            exception.NewError(
                "invalid mode",
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
    }
    if "" == role {
        role = config.RoleAll
    }

    if config.RoleWeb != role && config.RoleWorker != role && config.RoleAll != role {
        exception.Panic(
            exception.NewError(
                "invalid role",
                exceptioncontract.Context{
                    "role": role,
                },
                nil,
            ),
        )
    }

    return NewRuntimeFlagsWithRole(mode, role)
}

func parseRuntimeFlagFromArguments(arguments []string, flagName string) string {
    parsedValue := ""

    for index := 1; index < len(arguments); index++ {
        argument := strings.TrimSpace(arguments[index])
        if "" == argument {
            continue
        }

        flagValue, matched, consumeNext := parseRuntimeFlagValue(argument, flagName)
        if false == matched {
            continue
        }

        if true == consumeNext {
            if index+1 < len(arguments) {
                nextValue := strings.TrimSpace(arguments[index+1])
                if "" != nextValue && false == strings.HasPrefix(nextValue, "-") {
                    parsedValue = nextValue
                    index++
                }
            }

            continue
        }

        if "" != flagValue {
            parsedValue = flagValue
        }
    }

    return parsedValue
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
            skipNext = consumeNext
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

    cleanedArguments := make([]string, 0, len(originalArguments))
    cleanedArguments = append(cleanedArguments, originalArguments[0])

    skipNext := false

    for index := 1; index < len(originalArguments); index++ {
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
            skipNext = consumeNext
            continue
        }

        cleanedArguments = append(cleanedArguments, originalArguments[index])
    }

    os.Args = cleanedArguments
}
