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
}

func NewRuntimeFlags(mode string) *RuntimeFlags {
    return &RuntimeFlags{
        mode: mode,
    }
}

func (instance *RuntimeFlags) Mode() string {
    return instance.mode
}

func ParseRuntimeFlags(defaultMode string) *RuntimeFlags {
    parsedMode := ""
    arguments := os.Args

    for index := 1; index < len(arguments); index++ {
        argument := strings.TrimSpace(arguments[index])
        if "" == argument {
            continue
        }

        modeValue, matched, consumeNext := parseModeFlagValue(argument)
        if true == matched {
            if true == consumeNext {
                if index+1 < len(arguments) {
                    nextValue := strings.TrimSpace(arguments[index+1])
                    if "" != nextValue && false == strings.HasPrefix(nextValue, "-") {
                        parsedMode = nextValue
                        index++
                        continue
                    }
                }

                continue
            }

            if "" != modeValue {
                parsedMode = modeValue
            }

            continue
        }
    }

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

    return NewRuntimeFlags(mode)
}

func parseModeFlagValue(argument string) (string, bool, bool) {
    if "-mode" == argument || "--mode" == argument {
        return "", true, true
    }

    if true == strings.HasPrefix(argument, "-mode=") {
        return strings.TrimSpace(strings.TrimPrefix(argument, "-mode=")), true, false
    }

    if true == strings.HasPrefix(argument, "--mode=") {
        return strings.TrimSpace(strings.TrimPrefix(argument, "--mode=")), true, false
    }

    return "", false, false
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

        if "-mode" == argument || "--mode" == argument {
            skipNext = true
            continue
        }

        if true == strings.HasPrefix(argument, "-mode=") || true == strings.HasPrefix(argument, "--mode=") {
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

        if "-mode" == argument || "--mode" == argument {
            skipNext = true
            continue
        }

        if true == strings.HasPrefix(argument, "-mode=") || true == strings.HasPrefix(argument, "--mode=") {
            continue
        }

        cleanedArguments = append(cleanedArguments, originalArguments[index])
    }

    os.Args = cleanedArguments
}
