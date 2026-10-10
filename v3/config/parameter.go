package config

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
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
    /* the registration name, which identifies a parameter that has no environment key in an error */
    name string
    /* valueMutex guards value on its own: a service holding the *Parameter reads it without the configuration lock, while Resolve rewrites it under that lock, so writes go through storeValue and reads through loadValue */
    valueMutex sync.RWMutex
    value      any
    isDefault  bool
    /* atomic because MarkSecret marks under the configuration lock while a consumer holding the pointer asks IsSecret without it */
    isSecret atomic.Bool
    /* deferred marks a parameter whose template referenced a name not yet defined when the constructor resolved placeholders, left for the boot resolution. Atomic because the boot clears it under the configuration write lock while a consumer reads through loadValue without it. */
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

/* conversionName is the identity the shared parsers stamp into their error context: the environment key where one exists, the registration name otherwise. */
func (instance *Parameter) conversionName() string {
    if "" != instance.environmentKey {
        return instance.environmentKey
    }

    return instance.name
}

/* conversionCause hands the parse failure through for an ordinary parameter and withholds it for a secret one, since the parse errors quote the value they refused. */
func (instance *Parameter) conversionCause(causeErr error) error {
    if true == instance.isSecret.Load() {
        return nil
    }

    return causeErr
}

func (instance *Parameter) loadValue() any {
    /* a deferred parameter still holds its raw template, so every accessor refuses until the boot resolution settles the reference or fails the boot naming it */
    if true == instance.deferred.Load() {
        exception.Panic(
            exception.NewError(
                "cannot read a parameter whose resolution was deferred to boot; its value carries a template the boot resolution has not settled yet — a reference to a parameter that was not defined at construction, or a runtime registration made before boot",
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

/* Bool reads the value through internal.Bool, the parser its sibling accessors use, so a refusal is a ParseError naming the parameter, the target type and the value. */
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

/* Int reads the value through internal.Int, the parser its sibling accessors use, and narrows it: a value outside the int range is refused by name rather than truncated. */
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
