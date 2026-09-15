package contract

import (
    "errors"
    "fmt"
)

/* ErrFlagValueTypeMismatch identifies a validator input whose Go type does not match the declared flag kind. */
var ErrFlagValueTypeMismatch = errors.New("cli flag value type does not match the flag kind")

/* FlagKind defines the value types supported by the engine adapter, including custom Flag implementations. */
type FlagKind string

const (
    FlagKindString      FlagKind = "string"
    FlagKindBool        FlagKind = "bool"
    FlagKindInt         FlagKind = "int"
    FlagKindStringSlice FlagKind = "stringSlice"
)

/* FlagDefinition describes a flag to the engine adapter. Value must match Kind’s Go type. A nil Validator means no validation hook is installed. */
type FlagDefinition struct {
    Kind      FlagKind
    Name      string
    Usage     string
    Value     any
    Validator func(value any) error
}

/* Flag permits custom flag types built on one of the supported FlagKinds. */
type Flag interface {
    Definition() FlagDefinition
}

var _ Flag = (*StringFlag)(nil)
var _ Flag = (*BoolFlag)(nil)
var _ Flag = (*IntFlag)(nil)
var _ Flag = (*StringSliceFlag)(nil)

type StringFlag struct {
    Name      string
    Usage     string
    Value     string
    Validator func(value string) error
}

func (instance *StringFlag) Definition() FlagDefinition {
    return FlagDefinition{
        Kind:      FlagKindString,
        Name:      instance.Name,
        Usage:     instance.Usage,
        Value:     instance.Value,
        Validator: neutralValidator(FlagKindString, instance.Name, instance.Validator),
    }
}

type BoolFlag struct {
    Name      string
    Usage     string
    Value     bool
    Validator func(value bool) error
}

func (instance *BoolFlag) Definition() FlagDefinition {
    return FlagDefinition{
        Kind:      FlagKindBool,
        Name:      instance.Name,
        Usage:     instance.Usage,
        Value:     instance.Value,
        Validator: neutralValidator(FlagKindBool, instance.Name, instance.Validator),
    }
}

type IntFlag struct {
    Name      string
    Usage     string
    Value     int
    Validator func(value int) error
}

func (instance *IntFlag) Definition() FlagDefinition {
    return FlagDefinition{
        Kind:      FlagKindInt,
        Name:      instance.Name,
        Usage:     instance.Usage,
        Value:     instance.Value,
        Validator: neutralValidator(FlagKindInt, instance.Name, instance.Validator),
    }
}

type StringSliceFlag struct {
    Name      string
    Usage     string
    Value     []string
    Validator func(value []string) error
}

func (instance *StringSliceFlag) Definition() FlagDefinition {
    return FlagDefinition{
        Kind:      FlagKindStringSlice,
        Name:      instance.Name,
        Usage:     instance.Usage,
        Value:     instance.Value,
        Validator: neutralValidator(FlagKindStringSlice, instance.Name, instance.Validator),
    }
}

func neutralValidator[T any](kind FlagKind, flagName string, validator func(value T) error) func(value any) error {
    if nil == validator {
        return nil
    }

    return func(value any) error {
        typedValue, ok := value.(T)
        if false == ok {
            return fmt.Errorf(
                "%w: flag %q of kind %q received %T",
                ErrFlagValueTypeMismatch,
                flagName,
                kind,
                value,
            )
        }

        return validator(typedValue)
    }
}
