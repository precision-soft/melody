package config

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

type ParameterMap map[string]*Parameter

func NewParameter(environmentKey string, environmentValue any, value any, isDefault bool) *Parameter {
    return &Parameter{
        environmentKey:   environmentKey,
        environmentValue: environmentValue,
        value:            value,
        isDefault:        isDefault,
    }
}

type Parameter struct {
    environmentKey   string
    environmentValue any
    /* the registration name, for the parameters that have no environment key: a runtime parameter identified in an error only by its empty environmentKey was anonymous — "cannot convert" named nothing an operator could find */
    name string
    /* valueMutex guards value on its own, because the configuration lock does not reach the readers: a service handed the *Parameter reads it through the accessors below without ever touching the configuration, while Resolve rewrites every parameter under the configuration write lock. Two different locks around the same field are no lock at all, so the write side goes through storeValue and every read through loadValue. */
    valueMutex sync.RWMutex
    value      any
    isDefault  bool
    /* atomic because MarkSecret may mark a parameter under the configuration lock while a consumer that already holds the pointer asks IsSecret without it */
    isSecret atomic.Bool
    /* deferred marks a parameter whose template referenced a name that was not defined while the constructor resolved placeholders: the composition root registers its parameters between construction and boot, so the tolerant constructor pass leaves such a parameter for the boot resolution to settle in one order-independent batch. Atomic because the boot resolution clears the flag under the configuration write lock while a consumer that already holds the pointer reads through loadValue without it. */
    deferred atomic.Bool
}

func (instance *Parameter) diagnosticContext() map[string]any {
    context := map[string]any{
        "environmentKey": instance.environmentKey,
    }

    if "" != instance.name {
        context["parameterName"] = instance.name
    }

    return context
}

/* conversionName is the identity the shared parsers stamp into their error context: the environment key where one exists, the registration name otherwise — a runtime parameter has no environment key, and passing the empty key put a nameless parameterName inside the cause of an error whose outer context names the parameter, so the operator reading the cause chain concluded the parameter was anonymous. */
func (instance *Parameter) conversionName() string {
    if "" != instance.environmentKey {
        return instance.environmentKey
    }

    return instance.name
}

/* conversionCause hands the parse failure through for an ordinary parameter and withholds it for a secret one: the underlying strconv and parse errors quote the value they refused — exactly the right diagnostic for a mistyped pool size, and exactly the wrong log line for a credential that failed a conversion it was never meant for. */
func (instance *Parameter) conversionCause(causeErr error) error {
    if true == instance.isSecret.Load() {
        return nil
    }

    return causeErr
}

func (instance *Parameter) loadValue() any {
    /* a deferred parameter still holds its raw template: handing that out would serve %app.user% as though it were the value, so every accessor refuses loudly instead. The window is construction to boot — the boot resolution either settles the reference and clears the flag, or fails the boot naming it. */
    if true == instance.deferred.Load() {
        exception.Panic(
            exception.NewError(
                "cannot read a parameter whose resolution was deferred to boot; its value references a parameter that was not defined at construction, and the boot resolution has not run yet",
                instance.diagnosticContext(),
                nil,
            ),
        )
    }

    instance.valueMutex.RLock()
    defer instance.valueMutex.RUnlock()

    return instance.value
}

func (instance *Parameter) storeValue(value any) {
    instance.valueMutex.Lock()
    defer instance.valueMutex.Unlock()

    instance.value = value
}

func (instance *Parameter) EnvironmentKey() string {
    return instance.environmentKey
}

func (instance *Parameter) EnvironmentValue() any {
    return instance.environmentValue
}

func (instance *Parameter) Value() any {
    return instance.loadValue()
}

func (instance *Parameter) IsDefault() bool {
    return instance.isDefault
}

/* IsSecret reports whether the parameter was declared as holding a credential, either directly or by resolving a template that reads one. Commands that render the configuration redact such a parameter; the value itself is untouched. */
func (instance *Parameter) IsSecret() bool {
    return instance.isSecret.Load()
}

func (instance *Parameter) String() string {
    stringValue, ok := instance.loadValue().(string)
    if true == ok {
        return stringValue
    }

    return ""
}

func (instance *Parameter) MustString() string {
    value := instance.loadValue()

    stringValue, ok := value.(string)
    if true == ok {
        return stringValue
    }

    exception.Panic(
        exception.NewError(
            "cannot convert parameter value to string",
            mustStringContext(instance, value),
            nil,
        ),
    )

    return ""
}

/* Bool reads the value through the same parser its sibling accessors use, the last typed door that used to carry a grammar of its own. What it accepts and refuses is unchanged — the hand-written branches recognised exactly the shapes internal.Bool recognises — and what changes is what a refusal SAYS: a parameter holding a number, which is what RegisterRuntime("feature.flag", 1) hands over, was refused with a nil cause and a context carrying only the key, so the operator learned that something failed and nothing about what, while every sibling answered with a ParseError naming the parameter, the target type and the value. */
func (instance *Parameter) Bool() (bool, error) {
    boolValue, isSet, boolErr := internal.Bool(instance.loadValue(), instance.conversionName())
    if nil != boolErr || false == isSet {
        return false, exception.NewError(
            "cannot convert parameter value to bool",
            instance.diagnosticContext(),
            instance.conversionCause(boolErr),
        )
    }

    return boolValue, nil
}

/* Int reads the value through the same parser its sibling accessors use, so one grammar answers for every typed reading of a parameter: an int64 registered at runtime — what a caller writing RegisterRuntime("app.batch_size", int64(500)) hands over — converted through Float and refused through Int, on a value that is plainly a whole number. The one thing this door adds is the narrowing: internal.Int answers an int64 while an int is what a caller asked for, so a value outside the int range is refused by name rather than truncated, which is the silent corruption the shared parser already refuses for a float64 too wide to hold. */
func (instance *Parameter) Int() (int, error) {
    intValue, isSet, intErr := internal.Int(instance.loadValue(), instance.conversionName())
    if nil != intErr || false == isSet {
        return 0, exception.NewError(
            "cannot convert parameter value to int",
            instance.diagnosticContext(),
            instance.conversionCause(intErr),
        )
    }

    if int64(int(intValue)) != intValue {
        return 0, exception.NewError(
            "parameter value does not fit an int on this platform",
            instance.diagnosticContext(),
            nil,
        )
    }

    return int(intValue), nil
}

func (instance *Parameter) Float() (float64, error) {
    floatValue, isSet, floatErr := internal.Float64(instance.loadValue(), instance.conversionName())
    if nil != floatErr || false == isSet {
        return 0, exception.NewError(
            "cannot convert parameter value to float",
            instance.diagnosticContext(),
            instance.conversionCause(floatErr),
        )
    }

    return floatValue, nil
}

func (instance *Parameter) Duration() (time.Duration, error) {
    durationValue, isSet, durationErr := internal.Duration(instance.loadValue(), instance.conversionName())
    if nil != durationErr || false == isSet {
        return 0, exception.NewError(
            "cannot convert parameter value to duration",
            instance.diagnosticContext(),
            instance.conversionCause(durationErr),
        )
    }

    return durationValue, nil
}

func mustStringContext(instance *Parameter, value any) map[string]any {
    context := instance.diagnosticContext()
    context["valueType"] = fmt.Sprintf("%T", value)

    return context
}

var _ configcontract.Parameter = (*Parameter)(nil)
