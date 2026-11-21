package config

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

type ParameterMap map[string]*Parameter

func NewParameter(environmentKey string, template any, value any, isDefault bool) *Parameter {
    return &Parameter{
        environmentKey: environmentKey,
        template:       template,
        value:          value,
        isDefault:      isDefault,
    }
}

type Parameter struct {
    environmentKey string
    template any
    /* the registration name, which identifies a parameter that has no environment key in an error */
    name string
    /* valueMutex guards value on its own: a service holding the *Parameter reads it without the configuration lock, while Resolve rewrites it under that lock, so writes go through storeValue and reads through loadValue */
    valueMutex sync.RWMutex
    value      any
    isDefault  bool
    /* set when the application's override rewrote the template; written at construction or at the key's declaration, under the configuration lock */
    overridden bool
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

    if true == instance.overridden {
        context["overridden"] = true
    }

    return context
}

/* conversionName is the identity the shared parsers stamp into their error context: the environment key where one feeds the parameter, the registration name otherwise. */
func (instance *Parameter) conversionName() string {
    /* an overridden parameter does not read its key, so its name identifies it */
    if "" != instance.environmentKey && false == instance.overridden {
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

func (instance *Parameter) Template() any {
    return instance.template
}

func (instance *Parameter) Value() any {
    return instance.loadValue()
}

/* IsDefault reports whether the declared default spoke: no artifact set the parameter's key, or, under the key's rule, set it blank; an overridden parameter reads no default */
func (instance *Parameter) IsDefault() bool {
    return false == instance.overridden && true == instance.isDefault
}

func (instance *Parameter) IsOverridden() bool {
    return instance.overridden
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
