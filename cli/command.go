package cli

import (
    "context"
    "errors"
    "fmt"
    "io"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
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
            Action: func(ctx context.Context, commandContext *clicontract.CommandContext) error {
                writer := commandContext.Writer
                if nil == writer {
                    writer = io.Discard
                }

                /* in json mode the command writes one machine-readable document to this stream, so there is no banner; output.Meta carries the command, arguments, start time and duration. The final status is the exit code, since a shutdown failure found after the document was written cannot enter it. */
                resolvedOption := output.NormalizeOption(
                    output.ParseOptionFromCommand(commandContext),
                )

                if true == output.IsJsonFormat(resolvedOption.Format) {
                    writer = io.Discard
                }

                startedAt := time.Now()
                const logFiller = "======================================"

                /* the flag promises the absence of ansi sequences, and the banner is written to the same stream the command's own output goes to: a --no-color run redirected into a file must not carry escape codes around an output that honoured the flag */
                noColor := resolvedOption.NoColor

                /* the banner is decoration, which quiet governs: StandardFlags defaults it to true, DebugFlags to false, and a command declaring neither reads false */
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
                    /* the text embeds the command's own error, which routinely echoes downstream and client-derived values: escaped here, an embedded carriage return or escape sequence cannot repaint the status line as another verdict, and the no-color branch keeps the promise above — no escape codes reach a redirected file through the data either */
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

                /* printGreenVerdictLine is the finish form of printGreenStatusLine: the text on either side of the verdict is escaped as data and the verdict is coloured after, since a sanitiser handed an already coloured line cannot tell the author's escape sequence from the client's bytes. */
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

                /* a panic in the command leaves the path that assigns commandErr, so the finish banner reads it here; the panic is re-raised unchanged, keeping an *exception.ExitError's code. Nothing is closed on this path: the caller's defer closes the scope, and the recover handler that owns the exit closes the container after resolving its logger. */
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

                /* normalized through the interface: a command returning a concrete typed nil pointer hands over a non-nil interface; the scope's Close result, from the substitutable runtime contract, gets the same reading */
                runErr := normalizeCliError(copied.Run(runtimeInstance, commandContext))

                closeErrorByName := map[string]error{}

                /* the container is not closed here on either outcome: the recover handler that owns the exit resolves the final record's logger through it and closes it between the record and os.Exit. The scope is this action's to close and report. */
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

/* normalizeCliError reads the error through the interface: a command or a substituted runtime declared with a concrete error type hands back a typed nil in a non-nil interface, which is not a failure. */
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

    /* the exit code is resolved with errors.As, which matches the outermost ExitError in the chain: returning the aggregate unwrapped would hand the caller the command's own exit error instead, and the shutdown failures — carried only here — would never reach the log. A typed-nil link matches too and answers code 0, which NewExitError refuses with a panic, so the match is honoured only for a wrapper that carries one and the aggregate is returned plainly otherwise. */
    var exitError *exception.ExitError
    if true == errors.As(runErr, &exitError) && nil != exitError {
        return exception.NewExitError(exitError.ExitCode(), aggregatedErr)
    }

    return aggregatedErr
}
