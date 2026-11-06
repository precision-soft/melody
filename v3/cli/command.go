package cli

import (
    "context"
    "fmt"
    "io"
    "os"
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

func NewCommandContext(applicationName string, applicationDescription string) *clicontract.CommandContext {
    commandContext := &clicontract.CommandContext{
        Name:           applicationName,
        Usage:          applicationDescription,
        ExitErrHandler: inertExitHandler,
    }

    return commandContext
}

/* Register copies the root's streams at registration: a stream set on the root afterwards does not reach the command */
func Register(commandContext *clicontract.CommandContext, command clicontract.Command, runtimeInstance runtimecontract.Runtime) {
    if nil == commandContext {
        exception.Panic(
            exception.NewError("root cli command may not be nil", nil, nil),
        )
    }

    registerCommand(commandContext, command, runtimeInstance)
}

func registerCommand(rootCommand *clicontract.CommandContext, command clicontract.Command, runtimeInstance runtimecontract.Runtime) {
    if true == internal.IsNilInterface(command) {
        exception.Panic(
            exception.NewError("cli command may not be nil", nil, nil),
        )
    }

    if true == internal.IsNilInterface(runtimeInstance) {
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

    for _, existing := range rootCommand.Commands {
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

    flags := prepareCommandFlags(copied.Flags())

    rootCommand.Commands = append(
        rootCommand.Commands,
        &clicontract.CommandContext{
            Name:  normalizedCommandName,
            Usage: copied.Description(),
            Flags: flags,
            /* a positional "help" or "h" is the command's own argument, the --help flag still prints the usage */
            HideHelpCommand: true,
            Writer:          resolvedWriter(rootCommand.Writer),
            ErrWriter:       resolvedWriter(rootCommand.ErrWriter),
            Action: func(ctx context.Context, actionCommand *clicontract.CommandContext) error {
                return runCommandAction(actionCommand, copied, runtimeInstance, normalizedCommandName)
            },
        },
    )
}

func runCommandAction(
    commandContext *clicontract.CommandContext,
    command clicontract.Command,
    runtimeInstance runtimecontract.Runtime,
    commandName string,
) error {
    var writer io.Writer = io.Discard
    if false == internal.IsNilInterface(commandContext.Writer) {
        writer = commandContext.Writer
    }

    resolvedOption := output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )

    if true == output.IsJsonFormat(resolvedOption.Format) {
        writer = io.Discard
    }

    startedAt := time.Now()
    const logFiller = "======================================"

    quiet := resolvedOption.Quiet

    banner := commandBanner{writer: writer, noColor: resolvedOption.NoColor}

    if false == quiet {
        banner.printFullLine()

        banner.printStatusLine(
            AnsiBackgroundGreen,
            fmt.Sprintf(
                "%s [%s] [started] [%s] %s",
                logFiller,
                commandName,
                startedAt.Format(time.DateTime),
                logFiller,
            ),
        )

        banner.printFullLine()
    }

    var commandErr error

    defer func() {
        if true == quiet {
            return
        }

        finishedAt := time.Now()
        duration := finishedAt.Sub(startedAt)

        durationSecondsString := fmt.Sprintf("%.3fs", duration.Seconds())

        banner.printFullLine()

        verdict := "[success]"
        failed := nil != commandErr
        if true == failed {
            verdict = "[failed]"
        }

        banner.printLine(
            AnsiBackgroundGreen,
            fmt.Sprintf("%s [%s] [finished] ", logFiller, commandName),
            verdict,
            failed,
            fmt.Sprintf(" [%s] [duration=%s] %s", finishedAt.Format(time.DateTime), durationSecondsString, logFiller),
        )

        banner.printFullLine()
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
                "commandName": commandName,
            },
            nil,
        )

        panic(recoveredValue)
    }()

    runErr := normalizeCliError(command.Run(runtimeInstance, commandContext))

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
        errorBanner := commandBanner{writer: commandErrorWriter(commandContext), noColor: resolvedOption.NoColor}
        errorBanner.printStatusLine(AnsiBackgroundRed, fmt.Sprintf("[error] %s", aggregatedErr.Error()))
        return aggregatedErr
    }

    return nil
}

type commandBanner struct {
    writer  io.Writer
    noColor bool
}

func (instance commandBanner) printFullLine() {
    if true == instance.noColor {
        return
    }

    _, _ = fmt.Fprintf(
        instance.writer,
        "%s%s%s\n",
        AnsiBackgroundGreen,
        AnsiEraseLine,
        AnsiReset,
    )
}

func (instance commandBanner) printStatusLine(background string, text string) {
    instance.printLine(background, text, "", false, "")
}

/* printLine escapes the text as data, since it may echo client-derived values, and colours the verdict after that */
func (instance commandBanner) printLine(background string, textBeforeVerdict string, verdict string, failed bool, textAfterVerdict string) {
    escapedBefore := internal.EscapeControlCharacters(textBeforeVerdict)
    escapedAfter := internal.EscapeControlCharacters(textAfterVerdict)

    if true == instance.noColor {
        _, _ = fmt.Fprintf(instance.writer, "%s%s%s\n", escapedBefore, verdict, escapedAfter)

        return
    }

    colouredVerdict := verdict
    if true == failed {
        colouredVerdict = AnsiRed + verdict + AnsiWhite
    }

    _, _ = fmt.Fprintf(
        instance.writer,
        "%s%s\r%s%s%s%s%s\n",
        background,
        AnsiEraseLine,
        AnsiWhite,
        escapedBefore,
        colouredVerdict,
        escapedAfter,
        AnsiReset,
    )
}

func refuseRepeatedFlagSpellings(flags []clicontract.Flag) {
    declaredBy := map[string]string{}
    if nil != urfavecli.HelpFlag {
        for _, spelling := range urfavecli.HelpFlag.Names() {
            declaredBy[spelling] = urfavecli.HelpFlag.Names()[0]
        }
    }

    for _, flag := range flags {
        if true == internal.IsNilInterface(flag) {
            exception.Panic(
                exception.NewError("cli flag may not be nil", nil, nil),
            )
        }

        spellingList := flag.Names()
        flagName := ""
        if 0 < len(spellingList) {
            flagName = spellingList[0]
        }

        for index, spelling := range spellingList {
            if "" == spelling && 0 < index {
                exception.Panic(
                    exception.NewError("cli flag alias is empty", map[string]any{"flagName": flagName}, nil),
                )
            }

            if firstFlagName, declared := declaredBy[spelling]; true == declared {
                exception.Panic(
                    exception.NewError(
                        "cli flag spelling declared twice",
                        map[string]any{"spelling": spelling, "flagName": flagName, "firstFlagName": firstFlagName},
                        nil,
                    ),
                )
            }

            declaredBy[spelling] = flagName
        }
    }
}

/* inertExitHandler replaces the engine's os.Exit: its recover handler writes the final record and closes the container */
func inertExitHandler(handlerContext context.Context, handlerCommand *clicontract.CommandContext, handlerErr error) {
}

func resolvedWriter(writer io.Writer) io.Writer {
    if true == internal.IsNilInterface(writer) {
        return nil
    }

    return writer
}

func commandErrorWriter(command *clicontract.CommandContext) io.Writer {
    if false == internal.IsNilInterface(command.ErrWriter) {
        return command.ErrWriter
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

    /* the aggregate is returned wrapped so its shutdown failures reach the log; a typed-nil link would answer code 0 */
    if exitError, isExit := internal.ExitErrorInChain(runErr); true == isExit {
        return exception.NewExitError(exitError.ExitCode(), aggregatedErr)
    }

    return aggregatedErr
}
