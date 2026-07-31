package config

import (
    "fmt"
    "strconv"
    "strings"
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
    /* @important valueMutex guards value on its own, because the configuration lock does not reach the readers: a service handed the *Parameter reads it through the accessors below without ever touching the configuration, while Resolve rewrites every parameter under the configuration write lock. Two different locks around the same field are no lock at all, so the write side goes through storeValue and every read through loadValue. */
    valueMutex sync.RWMutex
    value      any
    isDefault  bool
    /* @important atomic because MarkSecret may mark a parameter under the configuration lock while a consumer that already holds the pointer asks IsSecret without it */
    isSecret atomic.Bool
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

/* conversionCause hands the parse failure through for an ordinary parameter and withholds it for a secret one: the underlying strconv and parse errors quote the value they refused — exactly the right diagnostic for a mistyped pool size, and exactly the wrong log line for a credential that failed a conversion it was never meant for. */
func (instance *Parameter) conversionCause(causeErr error) error {
    if true == instance.isSecret.Load() {
        return nil
    }

    return causeErr
}

func (instance *Parameter) loadValue() any {
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

func (instance *Parameter) Bool() (bool, error) {
    value := instance.loadValue()

    boolValue, ok := value.(bool)
    if true == ok {
        return boolValue, nil
    }

    stringValue, ok := value.(string)
    if true == ok {
        parsedValue, boolFromStringErr := internal.BoolFromString(stringValue)
        if nil != boolFromStringErr {
            return false, exception.NewError(
                "cannot convert parameter value to bool",
                instance.diagnosticContext(),
                instance.conversionCause(boolFromStringErr),
            )
        }

        return parsedValue, nil
    }

    return false, exception.NewError(
        "cannot convert parameter value to bool",
        instance.diagnosticContext(),
        nil,
    )
}

func (instance *Parameter) Int() (int, error) {
    value := instance.loadValue()

    intValue, ok := value.(int)
    if true == ok {
        return intValue, nil
    }

    stringValue, ok := value.(string)
    if true == ok {
        parsedValue, atoiErr := strconv.Atoi(strings.TrimSpace(stringValue))
        if nil != atoiErr {
            return 0, exception.NewError(
                "cannot convert parameter value to int",
                instance.diagnosticContext(),
                instance.conversionCause(atoiErr),
            )
        }

        return parsedValue, nil
    }

    return 0, exception.NewError(
        "cannot convert parameter value to int",
        instance.diagnosticContext(),
        nil,
    )
}

func (instance *Parameter) Float() (float64, error) {
    floatValue, isSet, floatErr := internal.Float64(instance.loadValue(), instance.environmentKey)
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
    durationValue, isSet, durationErr := internal.Duration(instance.loadValue(), instance.environmentKey)
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
