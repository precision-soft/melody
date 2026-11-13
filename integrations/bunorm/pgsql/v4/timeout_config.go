package pgsql

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

/* TimeoutConfig names every deadline the driver applies, so none governs invisibly; its zero value is the default. */
type TimeoutConfig struct {
    /* ConnectTimeout bounds the dial, the handshake, the post-build hook and the boot ping: 5s by default, at least 1ms, unlimited refused, since a down database must fail the open. */
    ConnectTimeout bound.Duration

    /* ReadTimeout is the per-operation socket read deadline of request traffic: 30s by default, at least 1ms, unlimited admitted as no deadline, which OpenForMigration writes for its own connections. */
    ReadTimeout bound.Duration

    /* WriteTimeout is the per-operation socket write deadline of request traffic: 30s by default, at least 1ms, unlimited admitted as no deadline, which OpenForMigration writes for its own connections. */
    WriteTimeout bound.Duration
}

var timeoutConnectTimeoutRule = bound.Rule[time.Duration]{
    Name:    "TimeoutConfig.ConnectTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var timeoutReadTimeoutRule = bound.Rule[time.Duration]{
    Name:              "TimeoutConfig.ReadTimeout",
    Default:           30 * time.Second,
    Minimum:           bound.TimeoutMinimum,
    UnlimitedAdmitted: true,
}

var timeoutWriteTimeoutRule = bound.Rule[time.Duration]{
    Name:              "TimeoutConfig.WriteTimeout",
    Default:           30 * time.Second,
    Minimum:           bound.TimeoutMinimum,
    UnlimitedAdmitted: true,
}

type resolvedTimeout struct {
    connectTimeout time.Duration
    readTimeout    bound.Resolved[time.Duration]
    writeTimeout   bound.Resolved[time.Duration]
}

func resolveTimeout(config TimeoutConfig) (resolvedTimeout, error) {
    connectTimeout, connectTimeoutErr := bound.Resolve(config.ConnectTimeout, timeoutConnectTimeoutRule)
    if nil != connectTimeoutErr {
        return resolvedTimeout{}, connectTimeoutErr
    }

    readTimeout, readTimeoutErr := bound.Resolve(config.ReadTimeout, timeoutReadTimeoutRule)
    if nil != readTimeoutErr {
        return resolvedTimeout{}, readTimeoutErr
    }

    writeTimeout, writeTimeoutErr := bound.Resolve(config.WriteTimeout, timeoutWriteTimeoutRule)
    if nil != writeTimeoutErr {
        return resolvedTimeout{}, writeTimeoutErr
    }

    return resolvedTimeout{
        connectTimeout: connectTimeout.Value,
        readTimeout:    readTimeout,
        writeTimeout:   writeTimeout,
    }, nil
}

/* lifted keeps the connect timeout and lifts the read and write deadlines, the timeouts OpenForMigration dials with. */
func (instance resolvedTimeout) lifted() resolvedTimeout {
    return resolvedTimeout{
        connectTimeout: instance.connectTimeout,
        readTimeout:    bound.Resolved[time.Duration]{Unlimited: true},
        writeTimeout:   bound.Resolved[time.Duration]{Unlimited: true},
    }
}

func (instance resolvedTimeout) context() map[string]any {
    return map[string]any{
        "connectTimeout": instance.connectTimeout.String(),
        "readTimeout":    instance.readTimeout.Limit().String(),
        "writeTimeout":   instance.writeTimeout.Limit().String(),
    }
}
