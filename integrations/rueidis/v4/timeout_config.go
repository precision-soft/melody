package rueidis

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

/* TimeoutConfig bounds the provider's own ping round trips, at boot and through Ping, and nothing else: neither field reaches rueidis.ClientOption, so an ordinary command is bounded by ClientConfig.ConnWriteTimeout when its context carries no deadline, and a read-only one is retried past that for as long as its context allows. The budget that ends a command is the per-call timeout of the service issuing it. */
type TimeoutConfig struct {
    /* ConnectTimeout bounds the boot ping: 3s by default, at least 1ms, unlimited refused, since an unbounded ping against a store that never answers would hang the boot holding a client no one can close yet. */
    ConnectTimeout bound.Duration

    /* CommandTimeout bounds the Ping door: 3s by default, at least 1ms, unlimited refused. */
    CommandTimeout bound.Duration
}

var timeoutConnectTimeoutRule = bound.Rule[time.Duration]{
    Name:    "TimeoutConfig.ConnectTimeout",
    Default: 3 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var timeoutCommandTimeoutRule = bound.Rule[time.Duration]{
    Name:    "TimeoutConfig.CommandTimeout",
    Default: 3 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

type resolvedTimeout struct {
    connectTimeout time.Duration
    commandTimeout time.Duration
}

func resolveTimeout(config TimeoutConfig) (resolvedTimeout, error) {
    connectTimeout, connectTimeoutErr := bound.Resolve(config.ConnectTimeout, timeoutConnectTimeoutRule)
    if nil != connectTimeoutErr {
        return resolvedTimeout{}, connectTimeoutErr
    }

    commandTimeout, commandTimeoutErr := bound.Resolve(config.CommandTimeout, timeoutCommandTimeoutRule)
    if nil != commandTimeoutErr {
        return resolvedTimeout{}, commandTimeoutErr
    }

    return resolvedTimeout{
        connectTimeout: connectTimeout.Value,
        commandTimeout: commandTimeout.Value,
    }, nil
}
