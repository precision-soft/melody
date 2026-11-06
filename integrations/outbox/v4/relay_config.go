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

const defaultLockName = "melody:outbox:relay"

/* maximumBatchSize bounds one claim on both doors that take a batch size — the relay's BatchSize and the store's ClaimDueMessages limit; far above any sensible drain and far below what a mistyped configuration could ask a slice pre-allocation for. */
const maximumBatchSize = 100000

/* defaultMaxDeliveryAttemptsFactor multiplies the resolved MaxAttempts to derive the default MaxDeliveryAttempts, leaving generous head-room above the send-failure retry path so only a genuinely stuck (crash-poison) row trips the claim cap. */
const defaultMaxDeliveryAttemptsFactor = 2

const defaultRelayBackoffFactor = 2.0

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

var relayInitialBackoffRule = bound.Rule[time.Duration]{
    Name:    "RelayConfig.InitialBackoff",
    Default: 15 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var relayMaxBackoffRule = bound.Rule[time.Duration]{
    Name:    "RelayConfig.MaxBackoff",
    Default: 10 * time.Minute,
    Minimum: bound.TimeoutMinimum,
}

var relayLockReleaseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "RelayConfig.LockReleaseTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var relayResolutionTimeoutRule = bound.Rule[time.Duration]{
    Name:    "RelayConfig.ResolutionTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

type resolvedRelay struct {
    Repository          Repository
    Transport           messagebuscontract.Transport
    Codec               MessageCodec
    Locker              lockcontract.Locker
    LockName            string
    LockTtl             time.Duration
    VisibilityTimeout   time.Duration
    BatchSize           int
    MaxAttempts         int
    MaxDeliveryAttempts int
    InitialBackoff      time.Duration
    MaxBackoff          time.Duration
    BackoffFactor       float64
    LockReleaseTimeout  time.Duration
    ResolutionTimeout   time.Duration
}

func resolveRelayConfig(config RelayConfig) (resolvedRelay, error) {
    resolved := resolvedRelay{
        Repository: config.Repository,
        Transport:  config.Transport,
        Codec:      config.Codec,
        Locker:     config.Locker,
        LockName:   config.LockName,
    }

    if "" == resolved.LockName {
        resolved.LockName = defaultLockName
    }

    durations := []struct {
        limit  bound.Duration
        rule   bound.Rule[time.Duration]
        target *time.Duration
    }{
        {limit: config.LockTtl, rule: relayLockTtlRule, target: &resolved.LockTtl},
        {limit: config.VisibilityTimeout, rule: relayVisibilityTimeoutRule, target: &resolved.VisibilityTimeout},
        {limit: config.InitialBackoff, rule: relayInitialBackoffRule, target: &resolved.InitialBackoff},
        {limit: config.MaxBackoff, rule: relayMaxBackoffRule, target: &resolved.MaxBackoff},
        {limit: config.LockReleaseTimeout, rule: relayLockReleaseTimeoutRule, target: &resolved.LockReleaseTimeout},
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

    if resolved.MaxBackoff < resolved.InitialBackoff {
        return resolvedRelay{}, exception.NewError(
            "the initial backoff exceeds the maximum backoff",
            exceptioncontract.Context{
                "field":    "RelayConfig.InitialBackoff",
                "value":    resolved.InitialBackoff.String(),
                "maximum":  resolved.MaxBackoff.String(),
                "cappedBy": "RelayConfig.MaxBackoff",
            },
            nil,
        )
    }

    resolved.BackoffFactor = config.BackoffFactor
    if 0 == resolved.BackoffFactor {
        resolved.BackoffFactor = defaultRelayBackoffFactor
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN factor through */
    if false == (resolved.BackoffFactor >= 1) {
        return resolvedRelay{}, exception.NewError(
            "the backoff factor is below 1",
            exceptioncontract.Context{
                "field": "RelayConfig.BackoffFactor",
                "value": strconv.FormatFloat(config.BackoffFactor, 'g', -1, 64),
            },
            nil,
        )
    }

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
