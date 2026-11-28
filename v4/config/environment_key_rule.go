package config

import (
    "fmt"
    "slices"
    "strings"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* LimitKeyRule reads a key as the framework reads its limit keys: empty is absent and takes the rule's default, unlimited only where the rule admits it, and a value outside the rule is refused naming the field; the rule's own Name is replaced by the field. */
func LimitKeyRule[T bound.Unit](rule bound.Rule[T]) configcontract.EnvironmentKeyRule {
    return limitKeyRule[T]{rule: rule}
}

type limitKeyRule[T bound.Unit] struct {
    rule bound.Rule[T]
}

func (instance limitKeyRule[T]) Check(text string, field string) error {
    limit := bound.Limit[T]{}
    if unmarshalErr := limit.UnmarshalText([]byte(text)); nil != unmarshalErr {
        return exception.NewError("the limit is not a value of its unit", exceptioncontract.Context{"field": field}, unmarshalErr)
    }

    rule := instance.rule
    rule.Name = field

    if _, resolveErr := bound.Resolve(limit, rule); nil != resolveErr {
        return resolveErr
    }

    return nil
}

func (instance limitKeyRule[T]) Describe() string {
    resolvedDefault, _ := bound.Resolve(bound.Limit[T]{}, instance.rule)

    maximum := "none"
    if 0 != instance.rule.Maximum {
        maximum = bound.Of(instance.rule.Maximum).String()
    }

    return fmt.Sprintf(
        "limit default=%s minimum=%s maximum=%s unlimited=%t",
        resolvedDefault.Limit().String(),
        bound.Of(instance.rule.Minimum).String(),
        maximum,
        instance.rule.UnlimitedAdmitted || instance.rule.DefaultUnlimited,
    )
}

/* SwitchKeyRule reads a boolean key as the framework reads its switch keys: an empty value is unset, and anything that is not a boolean is refused naming the field. */
func SwitchKeyRule() configcontract.EnvironmentKeyRule {
    return switchKeyRule{}
}

type switchKeyRule struct{}

func (instance switchKeyRule) Check(text string, field string) error {
    if "" == strings.TrimSpace(text) {
        return nil
    }

    if _, parseErr := internal.BoolFromString(text); nil != parseErr {
        return exception.NewError("the switch does not hold a boolean", exceptioncontract.Context{"field": field}, parseErr)
    }

    return nil
}

func (instance switchKeyRule) Describe() string {
    return "switch"
}

/* ChoiceKeyRule admits the empty value as unset and otherwise exactly one of admitted, whitespace included; an empty list admits no value, so it panics here, where the module builds its declaration. */
func ChoiceKeyRule(admitted []string) configcontract.EnvironmentKeyRule {
    if 0 == len(admitted) {
        exception.Panic(exception.NewError("a choice key rule admits no value", nil, nil))
    }

    return choiceKeyRule{admitted: slices.Clone(admitted)}
}

type choiceKeyRule struct {
    admitted []string
}

func (instance choiceKeyRule) Check(text string, field string) error {
    if "" == text || true == slices.Contains(instance.admitted, text) {
        return nil
    }

    return exception.NewError(
        "the value is not one of the admitted choices",
        exceptioncontract.Context{"field": field, "admitted": slices.Clone(instance.admitted)},
        nil,
    )
}

func (instance choiceKeyRule) Describe() string {
    return "choice " + strings.Join(instance.admitted, "|")
}

/* ResolveLimitParameter reads a limit parameter as the framework reads its limit keys, so a module's read and the boot check of its key are one parser: empty is absent and takes the rule's default; a refusal is "invalid environment value" naming the key, the rule's error naming the field in its chain. */
func ResolveLimitParameter[T bound.Unit](parameter configcontract.Parameter, environmentKey string, rule bound.Rule[T]) (bound.Resolved[T], error) {
    return resolveParameter(parameter, environmentKey, rule)
}

/* ParameterSwitch reads a boolean parameter as the framework reads its switch keys: an empty value is unset, and a value that is not a boolean is refused naming the key. */
func ParameterSwitch(parameter configcontract.Parameter, environmentKey string) (configcontract.Switch, error) {
    return checkedParameterSwitch(parameter, environmentKey)
}
