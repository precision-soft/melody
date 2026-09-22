package cli

import (
    "context"
    "fmt"
    "io"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    urfavecli "github.com/urfave/cli/v3"
)

/* This file is the only source in the v3 module that names the flag parsing engine. Everything above it — the command contract, the flags a command declares, the context a command reads — is melody's own, and the engine is reached exclusively through the two conversions below. The boundary is asserted by a test rather than left to habit, because a catch-up that reaches for an engine type somewhere else would rebuild the coupling in silence. */

/* newEngineFlag builds the engine's flag from what a melody flag says about itself. A kind the engine has no parser for is refused where the command is registered, naming the flag and the kind: a flag that cannot be built is a wiring mistake, and the alternative to a panic is a command whose flag silently does not exist. */
func newEngineFlag(flag clicontract.Flag) urfavecli.Flag {
    if nil == flag {
        exception.Panic(
            exception.NewError("cli flag may not be nil", nil, nil),
        )
    }

    definition := flag.Definition()

    var engineFlag urfavecli.Flag

    switch definition.Kind {
    case clicontract.FlagKindString:
        engineFlag = &urfavecli.StringFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[string](definition),
            Validator: engineFlagValidator[string](definition),
        }

    case clicontract.FlagKindBool:
        engineFlag = &urfavecli.BoolFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[bool](definition),
            Validator: engineFlagValidator[bool](definition),
        }

    case clicontract.FlagKindInt:
        /* the base is pinned to ten because the contract promises a plain decimal integer: left to the engine's default of zero the base is inferred from the literal, so --limit=010 read as eight, --limit=08 was refused as invalid syntax and --limit=0x10 read as sixteen — a zero-padded number from a shell loop or a cron entry is the ordinary case, and it has to mean what it says */
        engineFlag = &urfavecli.IntFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[int](definition),
            Validator: engineFlagValidator[int](definition),
            Config:    urfavecli.IntegerConfig{Base: 10},
        }

    case clicontract.FlagKindStringSlice:
        engineFlag = &urfavecli.StringSliceFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[[]string](definition),
            Validator: engineFlagValidator[[]string](definition),
        }
    }

    if nil == engineFlag {
        exception.Panic(
            exception.NewError(
                "cli flag kind is not supported",
                map[string]any{
                    "flagName": definition.Name,
                    "flagKind": string(definition.Kind),
                },
                nil,
            ),
        )
    }

    /* after the kind and the type of the default have been checked, so a mistyped default is refused under its own name and not as a validator refusal */
    refuseARefusedDefault(definition)

    return engineFlag
}

/* engineFlagValue reads the declared default in the type the kind names. A definition that carries no default at all answers the zero value, which is what a flag type written by hand and leaving Value unset means; a default of the wrong type is refused at the same place a wrong kind is, because it would otherwise be dropped and the flag would quietly default to zero. A declared default the flag's own validator refuses is refused here as well, by refuseARefusedDefault: the engine validates only a value it parsed from the command line, never the default it was handed, so without this the declared refusal would read as honoured while every invocation that leaves the flag out ran on the very value it refuses. */
func engineFlagValue[T any](definition clicontract.FlagDefinition) T {
    var zeroValue T

    if nil == definition.Value {
        return zeroValue
    }

    typedValue, ok := definition.Value.(T)
    if false == ok {
        exception.Panic(
            exception.NewError(
                "cli flag default value does not match the flag kind",
                map[string]any{
                    "flagName": definition.Name,
                    "flagKind": string(definition.Kind),
                },
                nil,
            ),
        )
    }

    return typedValue
}

/* engineFlagValidator hands the neutral validator to the engine in the typed shape it installs. A flag declaring none answers nil rather than a function that accepts everything: the engine tells the two apart — a nil validator is a flag with nothing to refuse, and the shape it installs is the one a reader of the engine's flag expects. The engine runs the validator on a value it parsed and on nothing else; the declared default is validated at registration, by refuseARefusedDefault, and the zero value a definition without a default means is validated by nobody, since no one declared it. */
func engineFlagValidator[T any](definition clicontract.FlagDefinition) func(value T) error {
    validator := definition.Validator
    if nil == validator {
        return nil
    }

    return func(value T) error {
        return validator(value)
    }
}

/* runDeclaredValidator runs the declared validator over the declared default and contains a validator that panics: the panic is the developer's own bug, but raw it names neither the flag nor the kind, where the three refusals beside it do — so it is answered as a refusal that says what exploded, and the registration then names the flag as for any refused default. */
func runDeclaredValidator(definition clicontract.FlagDefinition) (refusal error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* an exit error is re-raised unchanged, as every recovery boundary of the framework re-raises it: the exit code belongs to whoever owns the process boundary, and that boundary reads it by type assertion on the recovered value, where a refusal carrying it as a cause would be read as a plain failure */
        if exitErr, isExit := recoveredValue.(*exception.ExitError); true == isExit {
            panic(exitErr)
        }

        refusal = exception.NewError(
            "the flag's own validator panicked on the declared default: "+fmt.Sprint(recoveredValue),
            nil,
            exception.PanicCause(recoveredValue),
        )
    }()

    return definition.Validator(definition.Value)
}

/* refuseARefusedDefault runs the declared validator over the declared default, where the mistyped default beside it is refused: a default the flag's own validator refuses is the same wiring mistake, and left to the engine it would never be seen, because the engine validates only the values it parses. A definition that carries no default at all is not validated — the zero value it means is nobody's declaration — and a definition without a validator has nothing to refuse it with; the four shipped flag types always carry their default, the typed field, zero when it was left unset, so for them a validator that refuses the zero value is refused here too, since the command would run on that zero whenever the flag is left out. */
func refuseARefusedDefault(definition clicontract.FlagDefinition) {
    if nil == definition.Value || nil == definition.Validator {
        return
    }

    refusal := runDeclaredValidator(definition)
    if nil == refusal {
        return
    }

    exception.Panic(
        exception.NewError(
            "cli flag default value is refused by the flag's own validator",
            map[string]any{
                "flagName": definition.Name,
                "flagKind": string(definition.Kind),
                "refusal":  refusal.Error(),
            },
            refusal,
        ),
    )
}

/* inertExitHandler is what both doors onto the engine install in place of its default, which ends the process itself — through os.Exit — on any error a command returns. Melody owns the process exit: the recover handler of the cli run resolves the final record's logger and closes the container between that record and the exit, and an engine that exited first would take the journal with it. There is deliberately no door to put the default back. */
func inertExitHandler(handlerContext context.Context, handlerCommand *urfavecli.Command, handlerErr error) {
}

/* engineContext answers melody's command context over one engine command — the root when the engine is dispatching help, the sub-command when it is dispatching an action. */
type engineContext struct {
    command *urfavecli.Command
    writer  io.Writer
}

var _ clicontract.Context = (*engineContext)(nil)

/* newEngineContext resolves the output stream once, at the door: the engine leaves it nil on a command that was never given one, and every caller downstream would otherwise repeat the same guard on its first written line. */
func newEngineContext(command *urfavecli.Command) *engineContext {
    var writer io.Writer = io.Discard

    if nil != command && nil != command.Writer {
        writer = command.Writer
    }

    return &engineContext{
        command: command,
        writer:  writer,
    }
}

func (instance *engineContext) String(flagName string) string {
    return instance.command.String(flagName)
}

func (instance *engineContext) Bool(flagName string) bool {
    return instance.command.Bool(flagName)
}

func (instance *engineContext) Int(flagName string) int {
    return instance.command.Int(flagName)
}

func (instance *engineContext) StringSlice(flagName string) []string {
    values := instance.command.StringSlice(flagName)

    return copyStringSlice(values)
}

func (instance *engineContext) IsSet(flagName string) bool {
    return instance.command.IsSet(flagName)
}

func (instance *engineContext) Arguments() []string {
    return copyStringSlice(instance.command.Args().Slice())
}

func (instance *engineContext) Writer() io.Writer {
    return instance.writer
}

/* copyStringSlice hands back the caller's own backing array. Both readers above answer a slice the engine keeps holding, and a command that sorts or truncates what it was given would otherwise rewrite the parsed command line under every later reader of the same flag. */
func copyStringSlice(values []string) []string {
    if nil == values {
        return nil
    }

    copied := make([]string, len(values))
    copy(copied, values)

    return copied
}
