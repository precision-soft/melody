package contract

import (
    "errors"
    "strings"
    "testing"
)

func TestFlagDefinition_CarriesTheKindNameUsageAndDefaultOfEveryShippedFlag(t *testing.T) {
    definitions := []struct {
        flag         Flag
        expectedKind FlagKind
        expectedName string
    }{
        {&StringFlag{Name: "format", Usage: "output format", Value: "table"}, FlagKindString, "format"},
        {&BoolFlag{Name: "quiet", Usage: "suppress headers", Value: true}, FlagKindBool, "quiet"},
        {&IntFlag{Name: "limit", Usage: "max items", Value: 7}, FlagKindInt, "limit"},
        {&StringSliceFlag{Name: "role", Usage: "roles", Value: []string{"admin"}}, FlagKindStringSlice, "role"},
    }

    expectedValues := []any{"table", true, 7, []string{"admin"}}

    for index, row := range definitions {
        definition := row.flag.Definition()

        if row.expectedKind != definition.Kind {
            t.Fatalf("expected kind %q, got %q", row.expectedKind, definition.Kind)
        }
        if row.expectedName != definition.Name {
            t.Fatalf("expected name %q, got %q", row.expectedName, definition.Name)
        }
        if "" == definition.Usage {
            t.Fatalf("expected the usage of %q to travel with the definition", definition.Name)
        }

        if FlagKindStringSlice == definition.Kind {
            typedValue, ok := definition.Value.([]string)
            expected := expectedValues[index].([]string)
            if false == ok || len(expected) != len(typedValue) || expected[0] != typedValue[0] {
                t.Fatalf("expected the declared default %v, got %v", expected, definition.Value)
            }

            continue
        }

        if expectedValues[index] != definition.Value {
            t.Fatalf("expected the declared default %v, got %v", expectedValues[index], definition.Value)
        }
    }
}

/* a flag that declares no validator must answer nil rather than a function that accepts everything: the engine tells the two apart, and one that always passes would validate the declared default too */
func TestFlagDefinition_AFlagWithoutAValidatorAnswersNoValidator(t *testing.T) {
    for _, flag := range []Flag{
        &StringFlag{Name: "format"},
        &BoolFlag{Name: "quiet"},
        &IntFlag{Name: "limit"},
        &StringSliceFlag{Name: "role"},
    } {
        if nil != flag.Definition().Validator {
            t.Fatalf("expected %q to declare no validator", flag.Definition().Name)
        }
    }
}

func TestFlagDefinition_TheNeutralValidatorCallsTheTypedOne(t *testing.T) {
    refusal := errors.New("refused")

    observed := ""
    flag := &StringFlag{
        Name: "format",
        Validator: func(value string) error {
            observed = value

            return refusal
        },
    }

    validationErr := flag.Definition().Validator("yaml")

    if false == errors.Is(validationErr, refusal) {
        t.Fatalf("expected the typed refusal to travel out, got %v", validationErr)
    }
    if "yaml" != observed {
        t.Fatalf("expected the typed validator to be handed the value, got %q", observed)
    }
}

/* a value of the wrong type reports a wiring mistake in an adapter or in a hand-written flag type: swallowed, the validator would quietly pass every value it could not read */
func TestFlagDefinition_TheNeutralValidatorRefusesAValueOfAnotherType(t *testing.T) {
    called := false
    flag := &IntFlag{
        Name: "limit",
        Validator: func(value int) error {
            called = true

            return nil
        },
    }

    validationErr := flag.Definition().Validator("seven")

    if false == errors.Is(validationErr, ErrFlagValueTypeMismatch) {
        t.Fatalf("expected ErrFlagValueTypeMismatch, got %v", validationErr)
    }
    if false == strings.Contains(validationErr.Error(), "limit") {
        t.Fatalf("expected the refusal to name the flag, got %q", validationErr.Error())
    }
    if false == strings.Contains(validationErr.Error(), string(FlagKindInt)) {
        t.Fatalf("expected the refusal to name the kind, got %q", validationErr.Error())
    }
    if true == called {
        t.Fatalf("expected the typed validator not to be reached")
    }
}
