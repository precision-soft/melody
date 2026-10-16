package cli

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/cli/output"
)

/* NormalizeVerbosityArguments rewrites the repeated short verbosity flag, -v, -vv, -vvv, into the level the cli library reads, --verbosity=N, since the library has no notion of a repeated letter. Every other argument passes through untouched, and so does everything after the "--" terminator, which belongs to the command. DispatchCommand and the application's console entry both call it; a second pass changes nothing. */
func NormalizeVerbosityArguments(arguments []string) []string {
    if 0 == len(arguments) {
        return arguments
    }

    normalized := make([]string, 0, len(arguments))
    stopNormalization := false

    for _, argument := range arguments {
        if true == stopNormalization {
            normalized = append(normalized, argument)
            continue
        }

        if "--" == argument {
            stopNormalization = true
            normalized = append(normalized, argument)
            continue
        }

        if true == strings.HasPrefix(argument, "-") && false == strings.HasPrefix(argument, "--") {
            isVerbosityShortFlag := true

            if 2 > len(argument) {
                isVerbosityShortFlag = false
            }

            if true == isVerbosityShortFlag && false == strings.HasPrefix(argument, "-v") {
                isVerbosityShortFlag = false
            }

            if true == isVerbosityShortFlag {
                for _, runeValue := range argument[2:] {
                    if 'v' != runeValue {
                        isVerbosityShortFlag = false
                        break
                    }
                }
            }

            if true == isVerbosityShortFlag {
                verbosityLevel := len(argument) - 1
                normalized = append(
                    normalized,
                    fmt.Sprintf(
                        "--%s=%d",
                        output.FlagNameVerbosity,
                        verbosityLevel,
                    ),
                )

                continue
            }
        }

        normalized = append(normalized, argument)
    }

    return normalized
}
