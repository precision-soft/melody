package bound

import (
    "time"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* TimeoutMinimum is the minimum of a timeout, a budget or a delay unless the field's rule names another. */
const TimeoutMinimum = time.Millisecond

/* TtlMinimum is the minimum of a ttl, a retention or an expiry unless the field's rule names another. */
const TtlMinimum = time.Second

/* Rule is how a component resolves one limit field: the name it is refused under, the default an absent limit takes, the smallest value admitted, the largest value admitted, and whether unlimited is admitted. DefaultUnlimited makes an absent limit resolve as unlimited in place of Default, for a field whose default is no bound; such a rule admits an explicit unlimited too. A zero Maximum sets no ceiling. */
type Rule[T Unit] struct {
    Name              string
    Default           T
    DefaultUnlimited  bool
    Minimum           T
    Maximum           T
    UnlimitedAdmitted bool
}

/* Resolved is a limit decided against its rule: unlimited, or the value the component applies. */
type Resolved[T Unit] struct {
    Value     T
    Unlimited bool
}

/* Limit answers the resolved limit as a limit again, unlimited or set to the value, so a value one component resolved is handed to another component's field in the state it was decided in. */
func (instance Resolved[T]) Limit() Limit[T] {
    if true == instance.Unlimited {
        return Limit[T]{state: limitStateUnlimited}
    }

    return Of(instance.Value)
}

/* Resolve decides the limit against the rule: absent takes the default, or unlimited under DefaultUnlimited, unlimited is admitted only where the rule admits it, and a value below the minimum or above a set maximum is refused. Each refusal names the field. */
func Resolve[T Unit](limit Limit[T], rule Rule[T]) (Resolved[T], error) {
    switch limit.state {
    case limitStateAbsent:
        if true == rule.DefaultUnlimited {
            return Resolved[T]{Unlimited: true}, nil
        }

        return Resolved[T]{Value: rule.Default}, nil
    case limitStateUnlimited:
        if false == rule.UnlimitedAdmitted && false == rule.DefaultUnlimited {
            return Resolved[T]{}, exception.NewError(
                "the limit does not admit unlimited",
                exceptioncontract.Context{
                    "field": rule.Name,
                },
                nil,
            )
        }

        return Resolved[T]{Unlimited: true}, nil
    }

    if rule.Minimum > limit.value {
        return Resolved[T]{}, exception.NewError(
            "the limit is below its minimum",
            exceptioncontract.Context{
                "field":   rule.Name,
                "value":   formatUnit(limit.value),
                "minimum": formatUnit(rule.Minimum),
            },
            nil,
        )
    }

    if 0 != rule.Maximum && rule.Maximum < limit.value {
        return Resolved[T]{}, exception.NewError(
            "the limit is above its maximum",
            exceptioncontract.Context{
                "field":   rule.Name,
                "value":   formatUnit(limit.value),
                "maximum": formatUnit(rule.Maximum),
            },
            nil,
        )
    }

    return Resolved[T]{Value: limit.value}, nil
}
