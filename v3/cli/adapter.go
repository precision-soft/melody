package cli

import (
    "context"
    "fmt"
    "io"
    "slices"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    urfavecli "github.com/urfave/cli/v3"
)

/* This file is the only source in the v3 module that names the flag parsing engine, reached exclusively through the two conversions below; a test asserts the boundary. */

/* newEngineFlag builds the engine's flag from what a melody flag says about itself. A kind the engine has no parser for is refused where the command is registered, naming the flag and the kind, rather than leaving a command whose flag does not exist. */
func newEngineFlag(flag clicontract.Flag) urfavecli.Flag {
    if true == internal.IsNilInterface(flag) {
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
            Required:  definition.Required,
            Aliases:   append([]string(nil), definition.Aliases...),
            Hidden:    definition.Hidden,
        }

    case clicontract.FlagKindBool:
        engineFlag = &urfavecli.BoolFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[bool](definition),
            Validator: engineFlagValidator[bool](definition),
            Required:  definition.Required,
            Aliases:   append([]string(nil), definition.Aliases...),
            Hidden:    definition.Hidden,
        }

    case clicontract.FlagKindInt:
        /* the base is pinned to ten because the contract promises a plain decimal integer: with the engine's inferred base, a zero-padded --limit=010 would read as eight */
        engineFlag = &urfavecli.IntFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[int](definition),
            Validator: engineFlagValidator[int](definition),
            Config:    urfavecli.IntegerConfig{Base: 10},
            Required:  definition.Required,
            Aliases:   append([]string(nil), definition.Aliases...),
            Hidden:    definition.Hidden,
        }

    case clicontract.FlagKindStringSlice:
        engineFlag = &urfavecli.StringSliceFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[[]string](definition),
            Validator: engineFlagValidator[[]string](definition),
            Required:  definition.Required,
            Aliases:   append([]string(nil), definition.Aliases...),
            Hidden:    definition.Hidden,
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

    /* after the kind and the type of the default are checked, so a mistyped default is refused under its own name */
    refuseARefusedDefault(definition)

    return engineFlag
}

/* engineFlagValue reads the declared default in the type the kind names; a definition without a default answers the zero value. A default of the wrong type is refused rather than dropped, and so is one the flag's own validator refuses (refuseARefusedDefault), since the engine validates only values it parsed. */
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

/* engineFlagValidator hands the neutral validator to the engine in its typed shape. A flag declaring none answers nil rather than a function that accepts everything, since the engine tells the two apart. The engine validates only parsed values; the declared default is validated at registration. */
func engineFlagValidator[T any](definition clicontract.FlagDefinition) func(value T) error {
    validator := definition.Validator
    if nil == validator {
        return nil
    }

    return func(value T) error {
        return validator(value)
    }
}

/* runDeclaredValidator runs the declared validator over the declared default and answers a validator that panics as a refusal naming what exploded, so the registration names the flag. */
func runDeclaredValidator(definition clicontract.FlagDefinition) (refusal error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        /* an exit error is re-raised unchanged, as at every recovery boundary of the framework: the process boundary reads its code by type assertion on the recovered value */
        if exitErr, isExit := internal.RecoveredExitError(recoveredValue); true == isExit {
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

/* refuseARefusedDefault runs the declared validator over the declared default: a default the flag's own validator refuses is a wiring mistake the engine would never see, since it validates only parsed values. A definition with no default or no validator is not checked; the four shipped flag types always carry their default, zero when unset. */
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

/* inertExitHandler replaces the engine's default, which calls os.Exit on any error a command returns: melody owns the process exit, and its recover handler writes the final record and closes the container before exiting. There is no door to put the default back. */
func inertExitHandler(handlerContext context.Context, handlerCommand *urfavecli.Command, handlerErr error) {
}

/* engineContext answers melody's command context over one engine command — the root when the engine is dispatching help, the sub-command when it is dispatching an action. */
type engineContext struct {
    command *urfavecli.Command
    writer  io.Writer
}

var _ clicontract.Context = (*engineContext)(nil)

/* newEngineContext resolves the output stream once, since the engine leaves it nil on a command never given one. A nil command is refused here, where every reader of the context passes. */
func newEngineContext(command *urfavecli.Command) *engineContext {
    if nil == command {
        exception.Panic(
            exception.NewError("cli engine context may not be built over a nil command", nil, nil),
        )
    }

    var writer io.Writer = io.Discard

    if false == internal.IsNilInterface(command.Writer) {
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

    return slices.Clone(values)
}

func (instance *engineContext) IsSet(flagName string) bool {
    return instance.command.IsSet(flagName)
}

func (instance *engineContext) Arguments() []string {
    return slices.Clone(instance.command.Args().Slice())
}

func (instance *engineContext) Writer() io.Writer {
    return instance.writer
}

