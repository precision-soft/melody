package cli

import (
    "context"
    "errors"
    "strings"
    "sync"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestPrepareCommandFlags_RefusesADefaultTheDeclaredValidatorRefuses(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        prepareCommandFlags([]clicontract.Flag{
            &clicontract.IntFlag{
                Name:             "limit",
                Value:            -1,
                ValidateDefaults: true,
                Validator: func(value int) error {
                    if 0 > value {
                        return errors.New("negative refused")
                    }

                    return nil
                },
            },
        })
    }, "cli flag default value is refused by the flag's own validator")
}

/* a flag that leaves ValidateDefaults false is not checked, as the engine checks only the values it parsed: a validator refusing the zero default of a flag the run leaves out or must give is a declaration v3.13.0 accepted */
func TestPrepareCommandFlags_AcceptsARefusedDefaultWithoutValidateDefaults(t *testing.T) {
    positiveLimit := func(value int) error {
        if 1 > value {
            return errors.New("a positive limit is required")
        }

        return nil
    }

    prepared := prepareCommandFlags([]clicontract.Flag{
        &clicontract.IntFlag{Name: "limit", Validator: positiveLimit},
        &clicontract.IntFlag{Name: "batch", Required: true, Validator: positiveLimit},
        &clicontract.StringFlag{Name: "email", Validator: func(value string) error {
            if false == strings.Contains(value, "@") {
                return errors.New("an address is required")
            }

            return nil
        }},
    })
    if 3 != len(prepared) {
        t.Fatalf("expected the three flags accepted, got %d", len(prepared))
    }
}

/* an unset default is the typed field's zero; under ValidateDefaults a validator refusing it is refused at registration like any other refused default */
func TestPrepareCommandFlags_RefusesAnUnsetDefaultTheDeclaredValidatorRefusesUnderValidateDefaults(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        prepareCommandFlags([]clicontract.Flag{
            &clicontract.IntFlag{
                Name:             "limit",
                ValidateDefaults: true,
                Validator: func(value int) error {
                    if 1 > value {
                        return errors.New("a positive limit is required")
                    }

                    return nil
                },
            },
        })
    }, "cli flag default value is refused by the flag's own validator")
}

/* the validator of a string-slice flag is handed the slice the engine builds from the declaration, a copy that is empty rather than nil when no default is declared: a validator refusing a nil slice accepts the flag here as the engine accepts it at every run */
func TestPrepareCommandFlags_HandsTheSliceValidatorTheEmptySliceTheEngineBuilds(t *testing.T) {
    var seen []string
    declared := []string{"a"}

    prepared := prepareCommandFlags([]clicontract.Flag{
        &clicontract.StringSliceFlag{
            Name:             "column",
            ValidateDefaults: true,
            Validator: func(values []string) error {
                seen = values
                if nil == values {
                    return errors.New("a nil slice is refused")
                }

                return nil
            },
        },
        &clicontract.StringSliceFlag{
            Name:             "row",
            Value:            declared,
            ValidateDefaults: true,
            Validator: func(values []string) error {
                values[0] = "written"

                return nil
            },
        },
    })
    if 2 != len(prepared) {
        t.Fatalf("expected the two accepted flags, got %d", len(prepared))
    }
    if nil == seen || 0 != len(seen) {
        t.Fatalf("expected the validator to see the empty non-nil slice the engine builds, got %#v", seen)
    }
    if "a" != declared[0] {
        t.Fatalf("expected the validator to receive a copy of the declared slice, the declaration reads %q", declared[0])
    }
}

/* each of the four flag types a melody command declares is checked under ValidateDefaults; a flag without a validator is not */
func TestPrepareCommandFlags_ChecksTheDefaultOfEveryFlagType(t *testing.T) {
    refusal := errors.New("refused")

    for name, flag := range map[string]clicontract.Flag{
        "string": &clicontract.StringFlag{Name: "format", Value: "xml", ValidateDefaults: true, Validator: func(string) error { return refusal }},
        "bool":   &clicontract.BoolFlag{Name: "quiet", Value: true, ValidateDefaults: true, Validator: func(bool) error { return refusal }},
        "slice":  &clicontract.StringSliceFlag{Name: "column", Value: []string{"a"}, ValidateDefaults: true, Validator: func([]string) error { return refusal }},
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredValue := recover()
                if nil == recoveredValue {
                    t.Fatalf("expected the refused default to panic")
                }

                recoveredErr, isError := recoveredValue.(error)
                if false == isError || false == errors.Is(recoveredErr, refusal) {
                    t.Fatalf("expected the validator's refusal as the cause, got %v", recoveredValue)
                }
            }()

            prepareCommandFlags([]clicontract.Flag{flag})
        })
    }

    prepared := prepareCommandFlags([]clicontract.Flag{
        &clicontract.StringFlag{Name: "format", Value: "xml"},
        &clicontract.IntFlag{Name: "limit", Value: 3, Validator: func(value int) error {
            if 0 > value {
                return refusal
            }

            return nil
        }},
    })
    if 2 != len(prepared) {
        t.Fatalf("expected the two accepted flags, got %d", len(prepared))
    }
}

func TestPrepareCommandFlags_ADefaultWhoseValidatorPanicsIsRefusedByFlagName(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the panicking validator to refuse the registration")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic, got %#v", recoveredValue)
        }

        if false == strings.Contains(recoveredErr.Error(), "refused by the flag's own validator") {
            t.Fatalf("expected the registration refusal, got %v", recoveredErr)
        }

        typedError, isTyped := recoveredErr.(*exception.Error)
        if false == isTyped {
            t.Fatalf("expected *exception.Error, got %T", recoveredErr)
        }

        if "batch" != typedError.Context()["flagName"] {
            t.Fatalf("expected the flag named, got %v", typedError.Context())
        }
    }()

    prepareCommandFlags([]clicontract.Flag{
        &clicontract.IntFlag{
            Name:             "batch",
            ValidateDefaults: true,
            Validator: func(int) error {
                panic("the validator broke")
            },
        },
    })
}

/* the engine writes into the instance it is handed what it parsed and never clears it; the fresh instance carries the declaration and none of that */
func TestFreshFlagInstance_CopiesTheDeclarationAndNotTheParseState(t *testing.T) {
    destination := 0
    original := &clicontract.IntFlag{
        Name:        "n",
        Aliases:     []string{"count"},
        Usage:       "a number",
        Value:       7,
        Destination: &destination,
        OnlyOnce:    true,
    }

    engineCommand := &clicontract.CommandContext{
        Name:           "probe",
        Flags:          []clicontract.Flag{original},
        ExitErrHandler: inertExitHandler,
        Action: func(context.Context, *clicontract.CommandContext) error {
            return nil
        },
    }
    if runErr := engineCommand.Run(context.Background(), []string{"probe", "--n=5"}); nil != runErr {
        t.Fatalf("expected the first parse to pass, got %v", runErr)
    }
    if false == original.IsSet() {
        t.Fatalf("expected the engine to mark the original as set")
    }

    fresh, isInt := freshFlagInstance(original).(*clicontract.IntFlag)
    if false == isInt {
        t.Fatalf("expected an IntFlag, got %T", freshFlagInstance(original))
    }
    if fresh == original {
        t.Fatalf("expected a new instance")
    }
    if true == fresh.IsSet() {
        t.Fatalf("expected the fresh instance to carry no parse state")
    }
    if "n" != fresh.Name || 1 != len(fresh.Aliases) || "count" != fresh.Aliases[0] || "a number" != fresh.Usage || 7 != fresh.Value || &destination != fresh.Destination || false == fresh.OnlyOnce {
        t.Fatalf("expected the declaration copied, got %+v", fresh)
    }
}

/* a flag that is not a pointer to a struct has nothing to copy and is handed over as it is */
func TestFreshFlagInstance_HandsANonPointerFlagOverAsItIs(t *testing.T) {
    flag := valueFlag{name: "value"}

    if flag != freshFlagInstance(flag) {
        t.Fatalf("expected the value flag unchanged")
    }
}

/* the reviewer's scenario: a command answering one stored slice is dispatched twice; the second dispatch, without the flag, reads the declared default and IsSet false, and a flag declared OnlyOnce accepts the second line's single spelling */
func TestDispatchCommand_ASecondDispatchOfAMemoizingCommandReadsFreshState(t *testing.T) {
    observedIsSet := []bool{}
    observedValue := []int{}

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue: []clicontract.Flag{
            &clicontract.IntFlag{Name: "n", Value: 1, OnlyOnce: true},
            &clicontract.StringFlag{Name: "s"},
        },
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            observedIsSet = append(observedIsSet, commandContext.IsSet("n"))
            observedValue = append(observedValue, commandContext.Int("n"))

            return nil
        },
    }

    runtimeInstance := newTestRuntime(t)

    for _, arguments := range [][]string{{"probe", "--n=5", "--s=x"}, {"probe"}, {"probe", "--n=6"}} {
        if runErr := DispatchCommand(context.Background(), command, runtimeInstance, arguments, nil); nil != runErr {
            t.Fatalf("expected %v to dispatch, got %v", arguments, runErr)
        }
    }

    if 3 != len(observedIsSet) || true != observedIsSet[0] || false != observedIsSet[1] || true != observedIsSet[2] {
        t.Fatalf("expected IsSet true, false, true, got %v", observedIsSet)
    }
    if 5 != observedValue[0] || 1 != observedValue[1] || 6 != observedValue[2] {
        t.Fatalf("expected the values 5, 1, 6, got %v", observedValue)
    }
}

/* two dispatches in flight parse into instances of their own; the race band reads this one */
func TestDispatchCommand_OverlappingDispatchesOfAMemoizingCommandDoNotShareState(t *testing.T) {
    var mutex sync.Mutex
    observed := map[string]int{}

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       []clicontract.Flag{&clicontract.IntFlag{Name: "n"}, &clicontract.StringFlag{Name: "s"}},
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            mutex.Lock()
            observed[commandContext.String("s")] = commandContext.Int("n")
            mutex.Unlock()

            return nil
        },
    }

    runtimeInstance := newTestRuntime(t)

    var wait sync.WaitGroup
    for index := 1; index <= 4; index++ {
        wait.Add(1)
        go func(index int) {
            defer wait.Done()

            arguments := []string{"probe", "--n=" + strings.Repeat("1", index), "--s=" + strings.Repeat("x", index)}
            if runErr := DispatchCommand(context.Background(), command, runtimeInstance, arguments, nil); nil != runErr {
                t.Errorf("expected %v to dispatch, got %v", arguments, runErr)
            }
        }(index)
    }
    wait.Wait()

    for index, expected := range map[string]int{"x": 1, "xx": 11, "xxx": 111, "xxxx": 1111} {
        if expected != observed[index] {
            t.Fatalf("expected each dispatch to read its own line, got %v", observed)
        }
    }
}

/* DispatchCommand refuses a default the flag's own validator refuses under ValidateDefaults, as registration does */
func TestDispatchCommand_RefusesADefaultTheDeclaredValidatorRefuses(t *testing.T) {
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue: []clicontract.Flag{
            &clicontract.IntFlag{Name: "batch", Value: -1, ValidateDefaults: true, Validator: func(value int) error {
                if 0 > value {
                    return errors.New("negative refused")
                }

                return nil
            }},
        },
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            return nil
        },
    }

    testhelper.AssertPanicsWithError(t, func() {
        _ = DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe"}, nil)
    }, "cli flag default value is refused by the flag's own validator")
}

/* without ValidateDefaults a run that leaves the flag out runs on the default the validator would refuse, as the engine has always answered, and a run that gives the flag is validated on the value it gives */
func TestDispatchCommand_RunsOnARefusedDefaultWithoutValidateDefaults(t *testing.T) {
    readLimit := -1
    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue: []clicontract.Flag{
            &clicontract.IntFlag{Name: "limit", Validator: func(value int) error {
                if 1 > value {
                    return errors.New("a positive limit is required")
                }

                return nil
            }},
        },
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext *clicontract.CommandContext) error {
            readLimit = commandContext.Int("limit")

            return nil
        },
    }

    dispatchErr := DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe"}, nil)
    if nil != dispatchErr {
        t.Fatalf("expected the run without the flag to succeed, got %v", dispatchErr)
    }

    if 0 != readLimit {
        t.Fatalf("expected the zero default read, got %d", readLimit)
    }

    dispatchErr = DispatchCommand(context.Background(), command, newTestRuntime(t), []string{"probe", "--limit=0"}, nil)
    if nil == dispatchErr {
        t.Fatalf("expected the given value the validator refuses to fail the run")
    }
}

/* valueFlag is an engine flag that is not a pointer to a struct */
type valueFlag struct {
    name string
}

func (instance valueFlag) Names() []string {
    return []string{instance.name}
}

func (instance valueFlag) IsSet() bool {
    return false
}

func (instance valueFlag) String() string {
    return instance.name
}

func (instance valueFlag) Get() any {
    return nil
}

func (instance valueFlag) Set(string, string) error {
    return nil
}

func (instance valueFlag) PreParse() error {
    return nil
}

func (instance valueFlag) PostParse() error {
    return nil
}
