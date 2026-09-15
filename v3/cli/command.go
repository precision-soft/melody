package cli

import (
    "context"
    "errors"
    "fmt"
    "io"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    urfavecli "github.com/urfave/cli/v3"
)

func Register(root *Root, command clicontract.Command, runtimeInstance runtimecontract.Runtime) {
    if nil == root {
        exception.Panic(
            exception.NewError("root cli command may not be nil", nil, nil),
        )
    }

    if nil == command {
        exception.Panic(
            exception.NewError("cli command may not be nil", nil, nil),
        )
    }

    if nil == runtimeInstance {
        exception.Panic(
            exception.NewError("runtime instance may not be nil in cli register", nil, nil),
        )
    }

    copied := command

    commandName := copied.Name()
    normalizedCommandName := strings.TrimSpace(commandName)

    if "" == normalizedCommandName {
        exception.Panic(
            exception.NewError(
                "cli command name may not be empty",
                map[string]any{
                    "commandName": commandName,
                },
                nil,
            ),
        )
    }

    for _, existing := range root.command.Commands {
        if normalizedCommandName == strings.TrimSpace(existing.Name) {
            exception.Panic(
                exception.NewError(
                    "cli command name already registered",
                    map[string]any{
                        "commandName": normalizedCommandName,
                    },
                    nil,
                ),
            )
        }
    }

    root.command.Commands = append(
        root.command.Commands,
        &urfavecli.Command{
            Name:  normalizedCommandName,
            Usage: copied.Description(),
            Flags: newEngineFlags(copied.Flags()),

            Writer:    root.writer,
            ErrWriter: root.errorWriter,
            Action: func(ctx context.Context, actionCommand *urfavecli.Command) error {
                commandContext := newEngineContext(actionCommand)

                writer := commandContext.Writer()

                resolvedOption := output.NormalizeOption(
                    output.ParseOptionFromCommand(commandContext),
                )

                if true == output.IsJsonFormat(resolvedOption.Format) {
                    writer = io.Discard
                }

                startedAt := time.Now()
                const logFiller = "======================================"

                noColor := resolvedOption.NoColor

                quiet := resolvedOption.Quiet

                printGreenFullLine := func(writer io.Writer) {
                    if true == noColor {
                        return
                    }

                    _, _ = fmt.Fprintf(
                        writer,
                        "%s%s%s\n",
                        AnsiBackgroundGreen,
                        AnsiEraseLine,
                        AnsiReset,
                    )
                }

                printGreenStatusLine := func(writer io.Writer, text string) {

                    text = internal.EscapeControlCharacters(text)

                    if true == noColor {
                        _, _ = fmt.Fprintf(writer, "%s\n", text)

                        return
                    }

                    _, _ = fmt.Fprintf(
                        writer,
                        "%s%s\r%s%s%s\n",
                        AnsiBackgroundGreen,
                        AnsiEraseLine,
                        AnsiWhite,
                        text,
                        AnsiReset,
                    )
                }

                printGreenVerdictLine := func(writer io.Writer, textBeforeVerdict string, verdict string, failed bool, textAfterVerdict string) {
                    escapedBefore := internal.EscapeControlCharacters(textBeforeVerdict)
                    escapedAfter := internal.EscapeControlCharacters(textAfterVerdict)

                    if true == noColor {
                        _, _ = fmt.Fprintf(writer, "%s%s%s\n", escapedBefore, verdict, escapedAfter)

                        return
                    }

                    colouredVerdict := verdict
                    if true == failed {
                        colouredVerdict = AnsiRed + verdict + AnsiWhite
                    }

                    _, _ = fmt.Fprintf(
                        writer,
                        "%s%s\r%s%s%s%s%s\n",
                        AnsiBackgroundGreen,
                        AnsiEraseLine,
                        AnsiWhite,
                        escapedBefore,
                        colouredVerdict,
                        escapedAfter,
                        AnsiReset,
                    )
                }

                printRedStatusLine := func(writer io.Writer, text string) {
                    text = internal.EscapeControlCharacters(text)

                    if true == noColor {
                        _, _ = fmt.Fprintf(writer, "%s\n", text)

                        return
                    }

                    _, _ = fmt.Fprintf(
                        writer,
                        "%s%s\r%s%s%s\n",
                        AnsiBackgroundRed,
                        AnsiEraseLine,
                        AnsiWhite,
                        text,
                        AnsiReset,
                    )
                }

                if false == quiet {
                    printGreenFullLine(writer)

                    printGreenStatusLine(
                        writer,
                        fmt.Sprintf(
                            "%s [%s] [started] [%s] %s",
                            logFiller,
                            normalizedCommandName,
                            startedAt.Format(time.DateTime),
                            logFiller,
                        ),
                    )

                    printGreenFullLine(writer)
                }

                var commandErr error

                defer func() {
                    if true == quiet {
                        return
                    }

                    finishedAt := time.Now()
                    duration := finishedAt.Sub(startedAt)

                    durationSecondsString := fmt.Sprintf("%.3fs", duration.Seconds())

                    printGreenFullLine(writer)

                    verdict := "[success]"
                    failed := nil != commandErr
                    if true == failed {
                        verdict = "[failed]"
                    }

                    printGreenVerdictLine(
                        writer,
                        fmt.Sprintf("%s [%s] [finished] ", logFiller, normalizedCommandName),
                        verdict,
                        failed,
                        fmt.Sprintf(" [%s] [duration=%s] %s", finishedAt.Format(time.DateTime), durationSecondsString, logFiller),
                    )

                    printGreenFullLine(writer)
                }()

                defer func() {
                    recoveredValue := recover()
                    if nil == recoveredValue {
                        return
                    }

                    commandErr = exception.NewError(
                        "cli command panicked",
                        map[string]any{
                            "commandName": normalizedCommandName,
                        },
                        nil,
                    )

                    panic(recoveredValue)
                }()

                runErr := normalizeCliError(copied.Run(runtimeInstance, commandContext))

                closeErrorByName := map[string]error{}

                scopeCloseErr := normalizeCliError(runtimeInstance.Scope().Close())
                if nil != scopeCloseErr {
                    closeErrorByName["scope"] = scopeCloseErr
                }

                aggregatedErr := aggregateCliErrors(runErr, closeErrorByName)
                if nil != aggregatedErr {
                    commandErr = aggregatedErr
                    printRedStatusLine(writer, fmt.Sprintf("[error] %s", aggregatedErr.Error()))
                    return aggregatedErr
                }

                return nil
            },
        },
    )
}

func newEngineFlags(flags []clicontract.Flag) []urfavecli.Flag {
    if 0 == len(flags) {
        return nil
    }

    engineFlags := make([]urfavecli.Flag, 0, len(flags))

    for _, flag := range flags {
        engineFlags = append(engineFlags, newEngineFlag(flag))
    }

    return engineFlags
}

func normalizeCliError(err error) error {
    if true == internal.IsNilInterface(err) {
        return nil
    }

    return err
}

func aggregateCliErrors(runErr error, closeErrorByName map[string]error) error {
    if 0 == len(closeErrorByName) {
        return runErr
    }

    keys := make([]string, 0, len(closeErrorByName))
    for key := range closeErrorByName {
        if "" == key {
            continue
        }

        keys = append(keys, key)
    }

    sort.Strings(keys)

    failures := make([]map[string]string, 0, len(keys))
    for _, key := range keys {
        err := closeErrorByName[key]
        if nil == err {
            continue
        }

        failures = append(
            failures,
            map[string]string{
                "name":    key,
                "message": err.Error(),
            },
        )
    }

    if 0 == len(failures) {
        return runErr
    }

    if nil == runErr {
        return exception.NewError(
            "failed to shutdown cli",
            map[string]any{
                "failures": failures,
            },
            nil,
        )
    }

    aggregatedErr := exception.NewError(
        "cli command failed with shutdown errors",
        map[string]any{
            "runError":    runErr.Error(),
            "failures":    failures,
            "hasFailures": true,
        },
        runErr,
    )

    var exitError *exception.ExitError
    if true == errors.As(runErr, &exitError) && nil != exitError {
        return exception.NewExitError(exitError.ExitCode(), aggregatedErr)
    }

    return aggregatedErr
}
