package outbox

import (
    "math"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

/* defaultLockNameSuffix follows the application namespace in the relay's default lock name, <namespace>:outbox:relay */
const defaultLockNameSuffix = ":outbox:relay"

/* maximumBatchSize bounds one claim on both doors that take a batch size — the relay's BatchSize and the store's ClaimDueMessages limit; far above any sensible drain and far below what a mistyped configuration could ask a slice pre-allocation for. */
const maximumBatchSize = 100000

/* defaultMaxDeliveryAttemptsFactor multiplies the resolved MaxAttempts to derive the default MaxDeliveryAttempts, leaving generous head-room above the send-failure retry path so only a genuinely stuck (crash-poison) row trips the claim cap. */
const defaultMaxDeliveryAttemptsFactor = 2


var relayLockTtlRule = bound.Rule[time.Duration]{
    Name:    "RelayConfig.LockTtl",
    Default: 30 * time.Second,
    Minimum: bound.TtlMinimum,
}

var relayVisibilityTimeoutRule = bound.Rule[time.Duration]{
    Name:    "RelayConfig.VisibilityTimeout",
    Default: 5 * time.Minute,
    Minimum: bound.TtlMinimum,
}

var relayBatchSizeRule = bound.Rule[int]{
    Name:    "RelayConfig.BatchSize",
    Default: 100,
    Minimum: 1,
    Maximum: maximumBatchSize,
}

var relayMaxAttemptsRule = bound.Rule[int]{
    Name:    "RelayConfig.MaxAttempts",
    Default: 12,
    Minimum: 1,
}

var relayMaxDeliveryAttemptsRule = bound.Rule[int]{
    Name:    "RelayConfig.MaxDeliveryAttempts",
    Minimum: 1,
}

var relayBackoffRule = bound.BackoffRule{
    Name:           "RelayConfig.Backoff",
    InitialDefault: 15 * time.Second,
    MaxDefault:     10 * time.Minute,
    FactorDefault:  2,
}

var relayUnavailableBackoffRule = bound.BackoffRule{
    Name:           "RelayConfig.UnavailableBackoff",
    InitialDefault: 15 * time.Second,
    MaxDefault:     time.Minute,
    FactorDefault:  2,
}

var relayResolutionTimeoutRule = bound.Rule[time.Duration]{
    Name:    "RelayConfig.ResolutionTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

type resolvedRelay struct {
    Repository            Repository
    Transport             messagebuscontract.Transport
    Codec                 MessageCodec
    Locker                lockcontract.Locker
    LockName              string
    LockTtl               time.Duration
    VisibilityTimeout     time.Duration
    BatchSize             int
    MaxAttempts           int
    MaxDeliveryAttempts   int
    Backoff               bound.ResolvedBackoff
    ResolutionTimeout     time.Duration
    UnavailableBackoff    bound.ResolvedBackoff
}

func resolveRelayConfig(config RelayConfig) (resolvedRelay, error) {
    resolved := resolvedRelay{
        Locker:     config.Locker,
        LockName:   config.LockName,
    }

    durations := []struct {
        limit  bound.Duration
        rule   bound.Rule[time.Duration]
        target *time.Duration
    }{
        {limit: config.LockTtl, rule: relayLockTtlRule, target: &resolved.LockTtl},
        {limit: config.VisibilityTimeout, rule: relayVisibilityTimeoutRule, target: &resolved.VisibilityTimeout},
        {limit: config.ResolutionTimeout, rule: relayResolutionTimeoutRule, target: &resolved.ResolutionTimeout},
    }

    for _, duration := range durations {
        value, resolveErr := bound.Resolve(duration.limit, duration.rule)
        if nil != resolveErr {
            return resolvedRelay{}, resolveErr
        }

        *duration.target = value.Value
    }

    batchSize, batchSizeErr := bound.Resolve(config.BatchSize, relayBatchSizeRule)
    if nil != batchSizeErr {
        return resolvedRelay{}, batchSizeErr
    }

    resolved.BatchSize = batchSize.Value

    maxAttempts, maxAttemptsErr := bound.Resolve(config.MaxAttempts, relayMaxAttemptsRule)
    if nil != maxAttemptsErr {
        return resolvedRelay{}, maxAttemptsErr
    }

    resolved.MaxAttempts = maxAttempts.Value

    maxDeliveryAttempts, maxDeliveryAttemptsErr := resolveMaxDeliveryAttempts(config.MaxDeliveryAttempts, resolved.MaxAttempts)
    if nil != maxDeliveryAttemptsErr {
        return resolvedRelay{}, maxDeliveryAttemptsErr
    }

    resolved.MaxDeliveryAttempts = maxDeliveryAttempts

    backoff, backoffErr := bound.ResolveBackoff(config.Backoff, relayBackoffRule)
    if nil != backoffErr {
        return resolvedRelay{}, backoffErr
    }

    resolved.Backoff = backoff

    unavailableBackoff, unavailableBackoffErr := bound.ResolveBackoff(config.UnavailableBackoff, relayUnavailableBackoffRule)
    if nil != unavailableBackoffErr {
        return resolvedRelay{}, unavailableBackoffErr
    }

    resolved.UnavailableBackoff = unavailableBackoff

    return resolved, nil
}

/* resolveMaxDeliveryAttempts derives the default from the resolved MaxAttempts, saturating at math.MaxInt, and refuses a present value at or below it: every send-failure retry re-claims the row and so counts toward the cap, so such a value would dead-letter a normally retrying row as poison after its first re-claim. */
func resolveMaxDeliveryAttempts(limit bound.Count, maxAttempts int) (int, error) {
    if true == limit.IsAbsent() {
        /* a MaxAttempts large enough that the product wraps past math.MaxInt would give a negative cap, dead-lettering every row on its first delivery; saturating keeps the cap strictly above MaxAttempts */
        if maxAttempts > math.MaxInt/defaultMaxDeliveryAttemptsFactor {
            return math.MaxInt, nil
        }

        return defaultMaxDeliveryAttemptsFactor * maxAttempts, nil
    }

    resolved, resolveErr := bound.Resolve(limit, relayMaxDeliveryAttemptsRule)
    if nil != resolveErr {
        return 0, resolveErr
    }

    if math.MaxInt == maxAttempts {
        return 0, exception.NewError(
            "no max delivery attempts can exceed the max attempts",
            exceptioncontract.Context{
                "field":     "RelayConfig.MaxDeliveryAttempts",
                "value":     strconv.Itoa(resolved.Value),
                "flooredBy": "RelayConfig.MaxAttempts",
            },
            nil,
        )
    }

    if resolved.Value <= maxAttempts {
        return 0, exception.NewError(
            "the max delivery attempts do not exceed the max attempts",
            exceptioncontract.Context{
                "field":     "RelayConfig.MaxDeliveryAttempts",
                "value":     strconv.Itoa(resolved.Value),
                "minimum":   strconv.Itoa(maxAttempts + 1),
                "flooredBy": "RelayConfig.MaxAttempts",
            },
            nil,
        )
    }

    return resolved.Value, nil
}
