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

/* NewCommandContext builds the root command an application registers its commands on. The engine's exit handler is replaced with an inert one: its default calls os.Exit on any error a command returns, and melody owns the process exit. */
func NewCommandContext(applicationName string, applicationDescription string) *clicontract.CommandContext {
    commandContext := &clicontract.CommandContext{
        Name:           applicationName,
        Usage:          applicationDescription,
        ExitErrHandler: inertExitHandler,
    }

    return commandContext
}

/* Register appends a command to a root command. The root's streams travel with the registration, since the engine defaults each command's own separately; a stream set on the root afterwards does not reach the command, which Root.SetWriter and Root.SetErrorWriter do. */
func Register(commandContext *clicontract.CommandContext, command clicontract.Command, runtimeInstance runtimecontract.Runtime) {
    if nil == commandContext {
        exception.Panic(
            exception.NewError("root cli command may not be nil", nil, nil),
        )
    }

    registerCommand(commandContext, command, runtimeInstance)
}

/* registerCommand is the registration Register and Root.Register share: the command is refused when nil, unnamed or named like one already registered, and its flags when they repeat a spelling. */
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

    /* a caller may append to the root's command list directly, so an entry may be nil */
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

    flags := copied.Flags()
    refuseRepeatedFlagSpellings(flags)

    rootCommand.Commands = append(
        rootCommand.Commands,
        &clicontract.CommandContext{
            Name:  normalizedCommandName,
            Usage: copied.Description(),
            Flags: flags,
            /* a positional "help" or "h" is the command's own argument, the --help flag still prints the usage */
            HideHelpCommand: true,
            Writer:          rootCommand.Writer,
            ErrWriter:       rootCommand.ErrWriter,
            Action: func(ctx context.Context, actionCommand *clicontract.CommandContext) error {
                return runCommandAction(actionCommand, copied, runtimeInstance, normalizedCommandName)
            },
        },
    )
}

/* runCommandAction is the action of every registered command: the banner framing it, the command itself, and the close of the scope it ran in, the three failures folded into the one error the tree answers. */
func runCommandAction(
    commandContext *clicontract.CommandContext,
    command clicontract.Command,
    runtimeInstance runtimecontract.Runtime,
    commandName string,
) error {
    /* the engine defaults the stream to standard output on every command it runs, so the guard is for an action invoked by hand on a context never run */
    var writer io.Writer = io.Discard
    if false == internal.IsNilInterface(commandContext.Writer) {
        writer = commandContext.Writer
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

    /* the banner is decoration, which quiet governs: StandardFlags defaults it to true, DebugFlags to false, and a command declaring neither reads false */
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

    /* a panic in the command leaves the path that assigns commandErr, so the finish banner reads it here; the panic is re-raised unchanged, keeping an *exception.ExitError's code. Nothing is closed on this path: the caller's defer closes the scope, and the recover handler that owns the exit closes the container after resolving its logger. */
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

    /* normalized through the interface: a command returning a concrete typed nil pointer hands over a non-nil interface; the scope's Close result, from the substitutable runtime contract, gets the same reading */
    runErr := normalizeCliError(command.Run(runtimeInstance, commandContext))

    closeErrorByName := map[string]error{}

    /* the container is not closed here on either outcome: the recover handler that owns the exit resolves the final record's logger through it and closes it between the record and os.Exit. The scope is this action's to close and report. */
    scopeCloseErr := normalizeCliError(runtimeInstance.Scope().Close())
    if nil != scopeCloseErr {
        closeErrorByName["scope"] = scopeCloseErr
    }

    aggregatedErr := aggregateCliErrors(runErr, closeErrorByName)
    if nil != aggregatedErr {
        commandErr = aggregatedErr
        /* the error line is written whatever quiet says and whatever the format: quiet governs decoration, StandardFlags defaults it to true, and for a failing command this line is its one answer on the terminal, so it goes to the error stream, never into the document on the output stream; the error itself still returns to the exit path */
        errorBanner := commandBanner{writer: commandErrorWriter(commandContext), noColor: resolvedOption.NoColor}
        errorBanner.printStatusLine(AnsiBackgroundRed, fmt.Sprintf("[error] %s", aggregatedErr.Error()))
        return aggregatedErr
    }

    return nil
}

/* commandBanner prints the frame a registered command runs inside, on the stream its output goes to; under --no-color it carries no escape codes. */
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

/* printLine escapes the text on either side of the verdict as data and colours the verdict after that: the text embeds the command's error, which may echo client-derived values, so an embedded carriage return or escape sequence cannot repaint the line, and the banner's own colour is not escaped with the data. */
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

/* refuseRepeatedFlagSpellings refuses a nil flag, a name or alias the command already declares, and an empty alias: the parser resolves a spelling to the first flag declaring it, silently. The engine mounts its own help flag on every command, so a flag declaring one of its spellings is refused too; a nil HelpFlag is the engine's way to mount none. MergeFlags refuses a repeated name between the standard flags and a command's own. */
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

/* inertExitHandler replaces the engine's default, which calls os.Exit on any error a command returns: melody owns the process exit, and its recover handler writes the final record and closes the container before exiting. */
func inertExitHandler(handlerContext context.Context, handlerCommand *clicontract.CommandContext, handlerErr error) {
}

/* commandErrorWriter is the stream a failure is reported on: the command's error writer, which the engine defaults to standard error on every command it runs, and standard error itself for one left nil. A failure is never written where the command's document goes. */
func commandErrorWriter(command *clicontract.CommandContext) io.Writer {
    if false == internal.IsNilInterface(command.ErrWriter) {
        return command.ErrWriter
    }

    return os.Stderr
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

    /* errors.As matches the outermost ExitError in the chain, so the aggregate is returned wrapped and the shutdown failures it alone carries reach the log; a typed-nil link would answer code 0, which NewExitError refuses, so the aggregate is returned plainly then */
    if exitError, isExit := internal.ExitErrorInChain(runErr); true == isExit {
        return exception.NewExitError(exitError.ExitCode(), aggregatedErr)
    }

    return aggregatedErr
}
