package cli

import (
    "context"
    "io"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    urfavecli "github.com/urfave/cli/v3"
)

func newEngineFlag(flag clicontract.Flag) urfavecli.Flag {
    if nil == flag {
        exception.Panic(
            exception.NewError("cli flag may not be nil", nil, nil),
        )
    }

    definition := flag.Definition()

    switch definition.Kind {
    case clicontract.FlagKindString:
        return &urfavecli.StringFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[string](definition),
            Validator: engineFlagValidator[string](definition),
        }

    case clicontract.FlagKindBool:
        return &urfavecli.BoolFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[bool](definition),
            Validator: engineFlagValidator[bool](definition),
        }

    case clicontract.FlagKindInt:
        return &urfavecli.IntFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[int](definition),
            Validator: engineFlagValidator[int](definition),
        }

    case clicontract.FlagKindStringSlice:
        return &urfavecli.StringSliceFlag{
            Name:      definition.Name,
            Usage:     definition.Usage,
            Value:     engineFlagValue[[]string](definition),
            Validator: engineFlagValidator[[]string](definition),
        }
    }

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

    return nil
}

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

func engineFlagValidator[T any](definition clicontract.FlagDefinition) func(value T) error {
    validator := definition.Validator
    if nil == validator {
        return nil
    }

    return func(value T) error {
        return validator(value)
    }
}

func inertExitHandler(handlerContext context.Context, handlerCommand *urfavecli.Command, handlerErr error) {
}

type engineContext struct {
    command *urfavecli.Command
    writer  io.Writer
}

var _ clicontract.Context = (*engineContext)(nil)

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

func copyStringSlice(values []string) []string {
    if nil == values {
        return nil
    }

    copied := make([]string, len(values))
    copy(copied, values)

    return copied
}
