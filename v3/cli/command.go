package cli

import (
    "context"
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

    /* the list is the tree's own and only this writes into it, so no entry can be nil */
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
            /* the tree's streams travel with the registration, since the engine defaults each command's own separately */
            Writer:    root.writer,
            ErrWriter: root.errorWriter,
            Action: func(ctx context.Context, actionCommand *urfavecli.Command) error {
                return runCommandAction(newEngineContext(actionCommand), copied, runtimeInstance, normalizedCommandName)
            },
        },
    )
}

/* runCommandAction is the action of every registered command: the banner framing it, the command itself, and the close of the scope it ran in, the three failures folded into the one error the tree answers. */
func runCommandAction(
    commandContext *engineContext,
    command clicontract.Command,
    runtimeInstance runtimecontract.Runtime,
    commandName string,
) error {
    writer := commandContext.Writer()

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
        banner.printStatusLine(AnsiBackgroundRed, fmt.Sprintf("[error] %s", aggregatedErr.Error()))
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

/* newEngineFlags converts a command's declared flags into the engine's own, one by one and in the order the command declared them, since a flag set is help output as well as a parser. */
func newEngineFlags(flags []clicontract.Flag) []urfavecli.Flag {
    if 0 == len(flags) {
        return nil
    }

    engineFlags := make([]urfavecli.Flag, 0, len(flags))

    /* the engine mounts its own help flag on every command, so a flag declaring one of its spellings is refused; a nil HelpFlag is the engine's way to mount none */
    declaredBy := map[string]string{}
    if nil != urfavecli.HelpFlag {
        for _, spelling := range urfavecli.HelpFlag.Names() {
            declaredBy[spelling] = urfavecli.HelpFlag.Names()[0]
        }
    }

    for _, flag := range flags {
        engineFlag := newEngineFlag(flag)
        refuseARepeatedFlagSpelling(flag.Definition(), declaredBy)
        engineFlags = append(engineFlags, engineFlag)
    }

    return engineFlags
}

/* refuseARepeatedFlagSpelling refuses a name or alias the command already declares, and an empty alias: the parser resolves a spelling to the first flag declaring it, silently. MergeFlags refuses a repeated name between the standard flags and a command's own. */
func refuseARepeatedFlagSpelling(definition clicontract.FlagDefinition, declaredBy map[string]string) {
    spellingList := append([]string{definition.Name}, definition.Aliases...)

    for index, spelling := range spellingList {
        if "" == spelling && 0 < index {
            exception.Panic(
                exception.NewError("cli flag alias is empty", map[string]any{"flagName": definition.Name}, nil),
            )
        }

        if firstFlagName, declared := declaredBy[spelling]; true == declared {
            exception.Panic(
                exception.NewError(
                    "cli flag spelling declared twice",
                    map[string]any{"spelling": spelling, "flagName": definition.Name, "firstFlagName": firstFlagName},
                    nil,
                ),
            )
        }

        declaredBy[spelling] = definition.Name
    }
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
