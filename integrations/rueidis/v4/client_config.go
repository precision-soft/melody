package rueidis

import (
    "crypto/tls"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/redis/rueidis"
)

/* ClientConfig is the client the provider builds; its zero value is the default, a client named nothing on database 0, verified by a boot ping, with no client-side cache. */
type ClientConfig struct {
    ClientName string

    SelectDb int

    TlsConfig *tls.Config

    /* DialTimeout bounds the dial of every connection: 5s by default, at least 1ms, unlimited refused, so the library's own dial default never governs a dial. */
    DialTimeout bound.Duration

    /* ConnWriteTimeout is the per-connection write deadline and the cadence of the library's background pings: 5s by default, at least 1ms, unlimited refused, since the library has no unbounded form. */
    ConnWriteTimeout bound.Duration

    /* BootPingDisabled skips the ping that verifies the store answers before Open returns; a dead address still refuses at client creation. */
    BootPingDisabled bool

    /* ClientSideCacheEnabled turns the library's client-side cache on, which needs RESP3. */
    ClientSideCacheEnabled bool

    /* ClientSideCacheBytes is the client-side cache's budget for each connection: 128 MiB by default, at least 1, unlimited refused, and refused when the cache is not enabled. */
    ClientSideCacheBytes bound.Count
}

var clientDialTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ClientConfig.DialTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var clientConnWriteTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ClientConfig.ConnWriteTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var clientSideCacheBytesRule = bound.Rule[int]{
    Name:    "ClientConfig.ClientSideCacheBytes",
    Default: rueidis.DefaultCacheBytes,
    Minimum: 1,
}

type resolvedClient struct {
    clientName             string
    selectDb               int
    tlsConfig              *tls.Config
    dialTimeout            time.Duration
    connWriteTimeout       time.Duration
    bootPingDisabled       bool
    clientSideCacheEnabled bool
    clientSideCacheBytes   int
}

func resolveClient(config ClientConfig) (resolvedClient, error) {
    dialTimeout, dialTimeoutErr := bound.Resolve(config.DialTimeout, clientDialTimeoutRule)
    if nil != dialTimeoutErr {
        return resolvedClient{}, dialTimeoutErr
    }

    connWriteTimeout, connWriteTimeoutErr := bound.Resolve(config.ConnWriteTimeout, clientConnWriteTimeoutRule)
    if nil != connWriteTimeoutErr {
        return resolvedClient{}, connWriteTimeoutErr
    }

    if false == config.ClientSideCacheEnabled && false == config.ClientSideCacheBytes.IsAbsent() {
        return resolvedClient{}, exception.NewError(
            "the client-side cache budget is set while the client-side cache is not enabled",
            exceptioncontract.Context{
                "field":     "ClientConfig.ClientSideCacheBytes",
                "enabledBy": "ClientConfig.ClientSideCacheEnabled",
            },
            nil,
        )
    }

    clientSideCacheBytes, clientSideCacheBytesErr := bound.Resolve(config.ClientSideCacheBytes, clientSideCacheBytesRule)
    if nil != clientSideCacheBytesErr {
        return resolvedClient{}, clientSideCacheBytesErr
    }

    return resolvedClient{
        clientName:             config.ClientName,
        selectDb:               config.SelectDb,
        tlsConfig:              config.TlsConfig,
        dialTimeout:            dialTimeout.Value,
        connWriteTimeout:       connWriteTimeout.Value,
        bootPingDisabled:       config.BootPingDisabled,
        clientSideCacheEnabled: config.ClientSideCacheEnabled,
        clientSideCacheBytes:   clientSideCacheBytes.Value,
    }, nil
}
