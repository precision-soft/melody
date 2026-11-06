package cli

import (
    "context"
    "fmt"
    "io"
    "os"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/cli/output"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NewCommandContext(applicationName string, applicationDescription string) *clicontract.CommandContext {
    commandContext := &clicontract.CommandContext{
        Name:  applicationName,
        Usage: applicationDescription,
    }

    return commandContext
}

func Register(commandContext *clicontract.CommandContext, command clicontract.Command, runtimeInstance runtimecontract.Runtime) {
    if nil == commandContext {
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

    for _, existing := range commandContext.Commands {
        if nil == existing {
            continue
        }

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

    commandContext.Commands = append(
        commandContext.Commands,
        &clicontract.CommandContext{
            Name:  normalizedCommandName,
            Usage: copied.Description(),
            Flags: copied.Flags(),
            /* a positional "help" or "h" is the command's own argument, the --help flag still prints the usage */
            HideHelpCommand: true,
            Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
                writer := commandContext.Writer
                if nil == writer {
                    writer = io.Discard
                }

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
                    /* the text embeds client-derived values: an embedded carriage return or escape sequence must not repaint the status line */
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

                /* the panic is re-raised unchanged, keeping an *exception.ExitError's code; nothing is closed on this path */
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

                /* the container is not closed here: the recover handler that owns the exit closes it between the record and os.Exit */
                scopeCloseErr := normalizeCliError(runtimeInstance.Scope().Close())
                if nil != scopeCloseErr {
                    closeErrorByName["scope"] = scopeCloseErr
                }

                aggregatedErr := aggregateCliErrors(runErr, closeErrorByName)
                if nil != aggregatedErr {
                    commandErr = aggregatedErr
                    /* written whatever quiet says and whatever the format, to the error stream, never into the document on the output stream */
                    printRedStatusLine(commandErrorWriter(commandContext), fmt.Sprintf("[error] %s", aggregatedErr.Error()))
                    return aggregatedErr
                }

                return nil
            },
        },
    )
}

func commandErrorWriter(commandContext *clicontract.CommandContext) io.Writer {
    if false == internal.IsNilInterface(commandContext.ErrWriter) {
        return commandContext.ErrWriter
    }

    return os.Stderr
}

/* a concrete error type hands back a typed nil in a non-nil interface, which is not a failure */
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

    /* the aggregate is wrapped: returned plain, the chain search finds the command's exit error, losing the shutdown failures */
    var exitError *exception.ExitError
    if true == exception.ChainAs(runErr, &exitError) && nil != exitError {
        return exception.NewExitError(exitError.ExitCode(), aggregatedErr)
    }

    return aggregatedErr
}
