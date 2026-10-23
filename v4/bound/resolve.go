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

/* Rule is how a component resolves one limit field: the name it is refused under, the default an absent limit takes, the smallest value admitted, and whether unlimited is admitted. */
type Rule[T Unit] struct {
    Name              string
    Default           T
    Minimum           T
    UnlimitedAdmitted bool
}

/* Resolved is a limit decided against its rule: unlimited, or the value the component applies. */
type Resolved[T Unit] struct {
    Value     T
    Unlimited bool
}

/* Resolve decides the limit against the rule: absent takes the default, unlimited is admitted only where the rule admits it, and a value below the minimum is refused. Each refusal names the field. */
func Resolve[T Unit](limit Limit[T], rule Rule[T]) (Resolved[T], error) {
    switch limit.state {
    case limitStateAbsent:
        return Resolved[T]{Value: rule.Default}, nil
    case limitStateUnlimited:
        if false == rule.UnlimitedAdmitted {
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

    return Resolved[T]{Value: limit.value}, nil
}
