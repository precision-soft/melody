package bound

import (
    "math"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* Backoff is the growing wait between attempts: Initial before the second attempt, multiplied by Factor for every attempt after it, capped at Max. A field left out takes its rule's default; a Factor of exactly 1 is a constant wait. */
type Backoff struct {
    Initial Duration
    Max     Duration
    Factor  float64
}

/* BackoffRule is how a component resolves one Backoff field: the name it is refused under and the defaults of its three fields. Both durations are at least TimeoutMinimum and never unlimited. */
type BackoffRule struct {
    Name           string
    InitialDefault time.Duration
    MaxDefault     time.Duration
    FactorDefault  float64
}

/* ResolvedBackoff is a Backoff decided against its rule. */
type ResolvedBackoff struct {
    Initial time.Duration
    Max     time.Duration
    Factor  float64
}

/* ResolveBackoff decides the backoff against the rule: a left-out field takes the rule's default, and a duration below TimeoutMinimum or unlimited, a Max below Initial and a Factor below 1 or NaN are refused, each naming the field under the rule's name. */
func ResolveBackoff(value Backoff, rule BackoffRule) (ResolvedBackoff, error) {
    initial, initialErr := Resolve(value.Initial, Rule[time.Duration]{
        Name:    rule.Name + ".Initial",
        Default: rule.InitialDefault,
        Minimum: TimeoutMinimum,
    })
    if nil != initialErr {
        return ResolvedBackoff{}, initialErr
    }

    maximum, maximumErr := Resolve(value.Max, Rule[time.Duration]{
        Name:    rule.Name + ".Max",
        Default: rule.MaxDefault,
        Minimum: TimeoutMinimum,
    })
    if nil != maximumErr {
        return ResolvedBackoff{}, maximumErr
    }

    if maximum.Value < initial.Value {
        return ResolvedBackoff{}, exception.NewError(
            "the initial backoff exceeds the maximum backoff",
            exceptioncontract.Context{
                "field":    rule.Name + ".Initial",
                "value":    initial.Value.String(),
                "maximum":  maximum.Value.String(),
                "cappedBy": rule.Name + ".Max",
            },
            nil,
        )
    }

    factor := value.Factor
    if 0 == factor {
        factor = rule.FactorDefault
    }

    /* NaN fails every comparison, so only the not-at-least-1 form refuses it */
    if false == (factor >= 1) {
        return ResolvedBackoff{}, exception.NewError(
            "the backoff factor is below 1",
            exceptioncontract.Context{
                "field": rule.Name + ".Factor",
                "value": strconv.FormatFloat(value.Factor, 'g', -1, 64),
            },
            nil,
        )
    }

    return ResolvedBackoff{
        Initial: initial.Value,
        Max:     maximum.Value,
        Factor:  factor,
    }, nil
}

/* Delay is the wait after the given attempt: Initial for the first, Initial × Factor^(attempt-1) after it, capped at Max; an attempt below 1 reads as the first. The growth is capped in float space, so no attempt count overflows into a negative wait, and a product that is not positive, as from a hand-built zero Initial, is Max rather than a wait that would spin. */
func (instance ResolvedBackoff) Delay(attempt int) time.Duration {
    if 1 > attempt {
        attempt = 1
    }

    delay := float64(instance.Initial) * math.Pow(instance.Factor, float64(attempt-1))
    if false == (0 < delay && delay < float64(instance.Max)) {
        return instance.Max
    }

    return time.Duration(delay)
}
