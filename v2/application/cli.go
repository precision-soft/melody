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

/* ParseRuntimeFlagsWithRole resolves the runtime mode and process role from os.Args. The mode: an explicit --mode/-mode wins, any other non-runtime argument implies cli, otherwise the configured default applies. The role: an explicit --role/-role wins over the configured default — the flag exists because melody reads configuration only from .env artifacts, never from the process environment, so a docker-compose deployment differentiates containers built from one image with `command: ["/app", "--role=worker"]`. Both flags are runtime-only: they never imply cli mode and are stripped before the cli framework parses the arguments. Any other value-taking flag placed before the command must use its --flag=value form — a foreign flag's arity is unknowable, so a space-separated value would read as the command name and end the runtime-flag region early (see subcommandBoundaryIndex). */
func ParseRuntimeFlagsWithRole(defaultMode string, defaultRole string) *RuntimeFlags {
    arguments := os.Args

    parsedMode, modeFlagPresent := parseRuntimeFlagFromArguments(arguments, "mode")
    parsedRole, roleFlagPresent := parseRuntimeFlagFromArguments(arguments, "role")

    mode := defaultMode
    if "" != parsedMode {
        mode = parsedMode
    } else if true == modeFlagPresent {
        /* an explicitly supplied but empty --mode (e.g. `--mode=` expanded from an unset env var, or a bare --mode that cannot consume a dash-leading next token) fails closed at the validation below instead of silently booting the configured default mode */
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
        /* an explicitly supplied but empty --role (e.g. `--role=` expanded from an unset env var, or a bare --role that cannot consume a dash-leading next token) fails closed at the validation below instead of silently widening to the most permissive RoleAll */
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

/* parseRuntimeFlagFromArguments returns the parsed value of a runtime flag and whether the flag was explicitly present. Scanning is confined to the runtime-flag region before the cli subcommand (see subcommandBoundaryIndex): --mode and --role always precede the command, so a --role/--mode that follows the command name is the command's own flag and is left for it to parse. When the flag is supplied more than once the last occurrence wins, even when that occurrence is explicitly empty — an earlier value never survives a later occurrence. The present flag distinguishes an absent flag from one supplied with an empty value (for instance `--role=`), so the caller can fail closed on the latter instead of silently applying a default. */
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
            /* a bare flag with no consumable next token contributes the empty value: last-wins must not let an earlier occurrence survive */
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

/* subcommandBoundaryIndex returns the index of the first token that ends the runtime-flag region: the cli subcommand name (the first positional argument), a bare "--" end-of-options terminator, or the end of the arguments. --mode and --role are documented to always precede the command, so everything from this index on — including a command's own --role/--mode flag — belongs to the command and must be left untouched. Runtime flags and the value a bare runtime flag consumes are skipped while scanning for the boundary; a non-runtime flag before the command (for instance a global --verbose) is part of the region and does not end it. A foreign flag's arity is unknowable here, so a non-runtime flag that takes a value must be written in its --flag=value form when it precedes the command: a space-separated value reads as the command name, ends the region, and silently discards any --role/--mode after it. */
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

/* runtimeFlagConsumesNextArgument mirrors parseRuntimeFlagFromArguments: a bare "--role" takes the following token as its value only when that token could BE a value. Consuming it unconditionally would delete the command's own next flag from os.Args — the parser would never have read it as the role, so the two must agree on what belongs to the runtime flag. */
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

    /* everything from the subcommand boundary on — the command name, its own flags (a --role/--mode among them) and any tokens after a "--" terminator — is kept verbatim so the cli framework receives it intact */
    cleanedArguments = append(cleanedArguments, originalArguments[boundary:]...)

    os.Args = cleanedArguments
}
