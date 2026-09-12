package contract

import (
    "errors"
    "fmt"
)

/* ErrFlagValueTypeMismatch is the cause of a validation refusal when the value handed to a flag's neutral validator is not of the type its kind names. It cannot happen for the flags melody ships — the engine adapter builds each kind's parser from the kind itself — so it reports a wiring mistake in an adapter or in a hand-written flag type, and reporting it is what stops such a mistake from being swallowed by a validator that quietly passed everything it could not read. */
var ErrFlagValueTypeMismatch = errors.New("cli flag value type does not match the flag kind")

/* FlagKind names the value a flag carries. It is the whole vocabulary the engine adapter switches on, which is why a flag type melody does not ship can still be declared: it describes itself with one of these kinds and the adapter knows how to parse it. */
type FlagKind string

const (
    FlagKindString      FlagKind = "string"
    FlagKindBool        FlagKind = "bool"
    FlagKindInt         FlagKind = "int"
    FlagKindStringSlice FlagKind = "stringSlice"
)

/* FlagDefinition is how a flag describes itself to the engine adapter. Value carries the default in the Go type the kind names, and Validator is the neutral form of the typed validators the four shipped flags declare — a nil validator means the flag declares none, which is not the same as one that accepts everything. */
type FlagDefinition struct {
    Kind      FlagKind
    Name      string
    Usage     string
    Value     any
    Validator func(value any) error
}

/* Flag is a command line flag a command declares. The single method is the description the engine adapter reads: melody ships the four kinds below, and a package that needs its own flag type — a port number that validates its range, a path that must exist — implements this interface over one of the kinds instead of asking melody for a new one. */
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

/* neutralValidator wraps a typed validator into the neutral form the definition carries. A flag that declares no validator answers nil rather than a function that accepts everything, because the engine tells the two apart: installing a validator that always passes is not the same as installing none. The type assertion is the wiring guard described on ErrFlagValueTypeMismatch — it refuses naming the flag and the kind instead of returning nil for a value the typed validator never saw. */
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
