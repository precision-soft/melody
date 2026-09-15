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

    name string

    valueMutex sync.RWMutex
    value      any
    isDefault  bool

    isSecret atomic.Bool

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

func (instance *Parameter) conversionName() string {
    if "" != instance.environmentKey {
        return instance.environmentKey
    }

    return instance.name
}

func (instance *Parameter) conversionCause(causeErr error) error {
    if true == instance.isSecret.Load() {
        return nil
    }

    return causeErr
}

func (instance *Parameter) loadValue() any {

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

/* IsSecret reports explicit secret marking or propagation through parameter templates. Rendering commands redact marked parameters without changing the stored value. */
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

/* Bool parses the parameter using the common boolean grammar. A refusal includes a ParseError with the parameter name, target type and supplied value. */
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
