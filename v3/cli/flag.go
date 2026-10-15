package cli

import (
    "fmt"
    "reflect"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
)

/* prepareCommandFlags is what registration and dispatch do to the flags a command answers before the engine sees them: a nil flag, a repeated spelling and an empty alias are refused by name, so is a default the flag's own validator refuses when the flag sets ValidateDefaults, and the engine is handed a fresh instance of each flag, carrying its declaration and no parse state. */
func prepareCommandFlags(flags []clicontract.Flag) []clicontract.Flag {
    refuseRepeatedFlagSpellings(flags)

    prepared := make([]clicontract.Flag, 0, len(flags))
    for _, flag := range flags {
        refuseARefusedDefault(flag)

        prepared = append(prepared, freshFlagInstance(flag))
    }

    return prepared
}

/* refuseARefusedDefault runs a flag's own validator over its declared default when the flag sets ValidateDefaults, for the four flag types a melody command declares, so the refusal the engine would answer at every run, without the flag's name, is answered at registration and at dispatch, by name. A flag that leaves ValidateDefaults false is not checked, as the engine checks only the values it parsed: its validator may refuse the zero default of a flag the run leaves out or must give. A flag without a validator, or of another engine type, is not checked either. A validator that panics refuses the flag as well, by name. */
func refuseARefusedDefault(flag clicontract.Flag) {
    var flagName string
    var refusal error

    switch typedFlag := flag.(type) {
    case *clicontract.IntFlag:
        flagName = typedFlag.Name
        if true == typedFlag.ValidateDefaults {
            refusal = runDeclaredValidator(typedFlag.Validator, typedFlag.Value)
        }
    case *clicontract.StringFlag:
        flagName = typedFlag.Name
        if true == typedFlag.ValidateDefaults {
            refusal = runDeclaredValidator(typedFlag.Validator, typedFlag.Value)
        }
    case *clicontract.BoolFlag:
        flagName = typedFlag.Name
        if true == typedFlag.ValidateDefaults {
            refusal = runDeclaredValidator(typedFlag.Validator, typedFlag.Value)
        }
    case *clicontract.StringSliceFlag:
        flagName = typedFlag.Name
        if true == typedFlag.ValidateDefaults {
            refusal = runDeclaredValidator(typedFlag.Validator, typedFlag.Value)
        }
    default:
        return
    }

    if nil == refusal {
        return
    }

    exception.Panic(
        exception.NewError(
            "cli flag default value is refused by the flag's own validator",
            map[string]any{
                "flagName": flagName,
                "refusal":  refusal.Error(),
            },
            refusal,
        ),
    )
}

/* runDeclaredValidator answers the validator's refusal of the value, a panic inside the validator read as a refusal too. */
func runDeclaredValidator[T any](validator func(T) error, value T) (refusal error) {
    if nil == validator {
        return nil
    }

    defer func() {
        recoveredValue := recover()
        if nil != recoveredValue {
            refusal = fmt.Errorf("the validator panicked: %v", recoveredValue)
        }
    }()

    return validator(value)
}

/* freshFlagInstance answers a new instance of the flag carrying its exported declaration — name, aliases, usage, default, validator, configuration, destination — and none of its parse state: the engine writes into the instance it is handed what it parsed, whether the flag was set and how many times, and never clears it, so a command answering one instance on every Flags() call would otherwise read the previous dispatch's state on the next and two dispatches in flight would race on it. A Destination still receives the parsed value, since the pointer is part of the declaration, so dispatches in flight write into the same one. A flag that is not a pointer to a struct is handed to the engine as it is. */
func freshFlagInstance(flag clicontract.Flag) clicontract.Flag {
    flagValue := reflect.ValueOf(flag)
    if reflect.Ptr != flagValue.Kind() || reflect.Struct != flagValue.Elem().Kind() {
        return flag
    }

    source := flagValue.Elem()
    fresh := reflect.New(source.Type())
    target := fresh.Elem()

    for index := 0; index < source.NumField(); index++ {
        if false == source.Type().Field(index).IsExported() {
            continue
        }

        target.Field(index).Set(source.Field(index))
    }

    freshFlag, isFlag := fresh.Interface().(clicontract.Flag)
    if false == isFlag {
        return flag
    }

    return freshFlag
}
