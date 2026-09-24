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

    /* the list is the tree's own and this is the only thing that writes into it, so every entry it holds was built four lines below and none of them can be nil */
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
            /* the tree's streams travel with the registration, because the engine defaults each command's own separately and a command registered after SetWriter would otherwise write past it */
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

    /* in json mode the command writes one machine-readable document to this same stream, so the banner would make it unparseable from the first byte; nothing is lost because output.Meta already carries the command, arguments, start time and duration. The final status is the exit code, not the document: the scope and container are closed after the document was written, so a shutdown failure discovered there can no longer enter it. */
    resolvedOption := output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )

    if true == output.IsJsonFormat(resolvedOption.Format) {
        writer = io.Discard
    }

    startedAt := time.Now()
    const logFiller = "======================================"

    /* the banner is decoration, and quiet is the documented governor of decoration: StandardFlags defaults it to true so a scripted invocation stays clean without asking, DebugFlags to false so an introspection command keeps its frame, and a command that declares neither reads false and keeps the banner it always had. The banner ignored the flag entirely, so the one output the contract promises quiet suppresses was the one it never touched — melody:routes:manifest piped into jq carried the frame into the document. */
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

    /* the finish banner reads commandErr, and a panic in the command leaves the linear path that assigns it: without this the unwinding ran the banner defer over a nil commandErr and printed [finished] [success] for a command that died. The panic itself is re-raised unchanged — an *exception.ExitError keeps its exit code — and the closes are deliberately NOT performed here on this path: the scope is closed by the caller's defer, and the container — on every path — by the recover handler that owns the exit, after it resolved the logger; closing the container here would hand that handler a closed logger and downgrade the fatal record to the emergency fallback. */
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

    /* a command that returns its error through a concrete typed pointer hands over a non-nil interface around a nil value: read as a failure it reaches Error() on a nil receiver on the printing line below. The same normalization guards the scope's Close result, which crosses the substitutable runtime contract. */
    runErr := normalizeCliError(command.Run(runtimeInstance, commandContext))

    closeErrorByName := map[string]error{}

    /* the container is deliberately not closed here, on either outcome — the reading the panic path above already had is the linear path's too: the recover handler that owns the process exit resolves the final record's logger through the container and closes it between the record and os.Exit, so a close here would downgrade a failed command's final record to the stderr fallback. The scope stays this action's to close, and its failure this action's to report. */
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

/* commandBanner prints the frame a registered command runs inside, on the stream the command's own output goes to. The flag promises the absence of ansi sequences, so a --no-color run redirected into a file carries no escape codes around an output that honoured it. */
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

/* printStatusLine is the line with no verdict in it */
func (instance commandBanner) printStatusLine(background string, text string) {
    instance.printLine(background, text, "", false, "")
}

/* printLine escapes the text on either side of the verdict as data and colours the verdict AFTER that. The text embeds the command's own error, which routinely echoes downstream and client-derived values: escaped, an embedded carriage return or escape sequence cannot repaint the line as another verdict. Coloured before the escaping, the banner's own escape sequence went through it together with the data, and every failed run with colour on — the default — printed \x1b[31m as literal text around [failed], while the no-color run, which never coloured the verdict, printed it right. A sanitiser handed an already formatted line cannot tell the author's bytes from the client's, so the presentation is added last. */
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

    /* the engine mounts its own help flag on every command, and a command's flag declaring one of its spellings is parsed in its place: -h stopped printing the usage and ran the command, in silence. A nil HelpFlag is the engine's own way to mount none, and every read the engine makes of it is guarded the same way. */
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

/* refuseARepeatedFlagSpelling refuses a spelling — a name or an alias — that the command already declares, and an empty alias: the parser resolves a spelling to the FIRST flag declaring it and says nothing about the second, so an alias that repeats another flag's name was parsed as that other flag while the help listed it under both. MergeFlags refuses a repeated name between the standard flags and a command's own; this is the door every declared spelling passes. */
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

/* normalizeCliError reads the error through the interface: a command or a substituted runtime declared with a concrete error type hands back a typed nil boxed into a non-nil interface, which would be treated as the failure it is not — and would panic the first line that renders it. */
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
    if exitError, isExit := internal.ExitErrorInChain(runErr); true == isExit {
        return exception.NewExitError(exitError.ExitCode(), aggregatedErr)
    }

    return aggregatedErr
}
