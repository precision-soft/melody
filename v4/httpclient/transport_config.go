package httpclient

import (
    "math"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* TransportConfig overrides the transport melody builds for a client. A field left out takes its default; a value below the field's minimum is refused, naming the field, and so is unlimited on a field that does not admit it. Every timeout but IdleConnectionTimeout refuses unlimited: the dial, TLS handshake, response header and 100-continue waits each guard against a peer that never answers, and KeepAlive has KeepAliveDisabled. */
type TransportConfig struct {
    /* DialTimeout bounds one dial: 10s by default, at least 1ms. */
    DialTimeout bound.Duration

    /* KeepAlive is the interval of the TCP keep-alive probes: 30s by default, at least 1s. KeepAliveDisabled turns the probes off and refuses a KeepAlive set beside it. */
    KeepAlive         bound.Duration
    KeepAliveDisabled bool

    /* MaxIdleConnections bounds the idle pool across hosts: 100 by default, at least 1, unlimited admitted. */
    MaxIdleConnections bound.Count

    /* MaxIdleConnectionsPerHost bounds the idle pool of one host: 100 by default, at least 1, unlimited admitted. It is decided on its own, whatever MaxIdleConnections holds; net/http applies both, so the smaller one wins. */
    MaxIdleConnectionsPerHost bound.Count

    /* IdleConnectionsDisabled keeps no connection idle, so every request dials its own over HTTP/1 and HTTP/2 alike, and refuses MaxIdleConnections, MaxIdleConnectionsPerHost or IdleConnectionTimeout set beside it. */
    IdleConnectionsDisabled bool

    /* IdleConnectionTimeout closes a connection idle that long: 90s by default, at least 1s, unlimited admitted. */
    IdleConnectionTimeout bound.Duration

    /* TlsHandshakeTimeout bounds the TLS handshake: 10s by default, at least 1ms. */
    TlsHandshakeTimeout bound.Duration

    /* ExpectContinueTimeout bounds the wait for a 100-continue: 1s by default; zero sends the body at once. */
    ExpectContinueTimeout bound.Duration

    /* ResponseHeaderTimeout bounds the wait for the response headers once the request is written: 15s by default, at least 1ms. */
    ResponseHeaderTimeout bound.Duration
}

/* resolvedTransportConfig is the transport configuration with every field decided, in net/http's terms. */
type resolvedTransportConfig struct {
    DialTimeout               time.Duration
    KeepAlive                 time.Duration
    MaxIdleConnections        int
    MaxIdleConnectionsPerHost int
    IdleConnectionTimeout     time.Duration
    TlsHandshakeTimeout       time.Duration
    ExpectContinueTimeout     time.Duration
    ResponseHeaderTimeout     time.Duration
    DisableKeepAlives         bool
}

const defaultMaxIdleConnections = 100

var (
    dialTimeoutRule = bound.Rule[time.Duration]{
        Name:    "TransportConfig.DialTimeout",
        Default: 10 * time.Second,
        Minimum: bound.TimeoutMinimum,
    }

    keepAliveRule = bound.Rule[time.Duration]{
        Name:    "TransportConfig.KeepAlive",
        Default: 30 * time.Second,
        Minimum: time.Second,
    }

    maxIdleConnectionsRule = bound.Rule[int]{
        Name:              "TransportConfig.MaxIdleConnections",
        Default:           defaultMaxIdleConnections,
        Minimum:           1,
        UnlimitedAdmitted: true,
    }

    maxIdleConnectionsPerHostRule = bound.Rule[int]{
        Name:              "TransportConfig.MaxIdleConnectionsPerHost",
        Default:           defaultMaxIdleConnections,
        Minimum:           1,
        UnlimitedAdmitted: true,
    }

    idleConnectionTimeoutRule = bound.Rule[time.Duration]{
        Name:              "TransportConfig.IdleConnectionTimeout",
        Default:           90 * time.Second,
        Minimum:           time.Second,
        UnlimitedAdmitted: true,
    }

    tlsHandshakeTimeoutRule = bound.Rule[time.Duration]{
        Name:    "TransportConfig.TlsHandshakeTimeout",
        Default: 10 * time.Second,
        Minimum: bound.TimeoutMinimum,
    }

    expectContinueTimeoutRule = bound.Rule[time.Duration]{
        Name:    "TransportConfig.ExpectContinueTimeout",
        Default: 1 * time.Second,
        Minimum: 0,
    }

    responseHeaderTimeoutRule = bound.Rule[time.Duration]{
        Name:    "TransportConfig.ResponseHeaderTimeout",
        Default: 15 * time.Second,
        Minimum: bound.TimeoutMinimum,
    }
)

func resolveTransportConfig(config TransportConfig) (resolvedTransportConfig, error) {
    var resolved resolvedTransportConfig

    dialTimeout, err := bound.Resolve(config.DialTimeout, dialTimeoutRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.DialTimeout = dialTimeout.Value

    if true == config.KeepAliveDisabled && false == config.KeepAlive.IsAbsent() {
        return resolvedTransportConfig{}, exception.NewError(
            "the keep-alive interval is set on a transport whose keep-alive is disabled",
            exceptioncontract.Context{
                "field": "TransportConfig.KeepAlive",
            },
            nil,
        )
    }

    keepAlive, err := bound.Resolve(config.KeepAlive, keepAliveRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.KeepAlive = keepAlive.Value

    if true == config.KeepAliveDisabled {
        /* net.Dialer reads a negative interval as keep-alive off */
        resolved.KeepAlive = -1
    }

    if true == config.IdleConnectionsDisabled {
        for _, beside := range []struct {
            name  string
            limit bound.Count
        }{
            {name: "TransportConfig.MaxIdleConnections", limit: config.MaxIdleConnections},
            {name: "TransportConfig.MaxIdleConnectionsPerHost", limit: config.MaxIdleConnectionsPerHost},
        } {
            if false == beside.limit.IsAbsent() {
                return resolvedTransportConfig{}, exception.NewError(
                    "an idle pool limit is set on a transport that keeps no idle connection",
                    exceptioncontract.Context{
                        "field": beside.name,
                    },
                    nil,
                )
            }
        }

        if false == config.IdleConnectionTimeout.IsAbsent() {
            return resolvedTransportConfig{}, exception.NewError(
                "an idle pool limit is set on a transport that keeps no idle connection",
                exceptioncontract.Context{
                    "field": "TransportConfig.IdleConnectionTimeout",
                },
                nil,
            )
        }
    }

    maxIdleConnections, err := bound.Resolve(config.MaxIdleConnections, maxIdleConnectionsRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.MaxIdleConnections = maxIdleConnections.Value
    if true == maxIdleConnections.Unlimited {
        /* net/http reads a zero total as no limit */
        resolved.MaxIdleConnections = 0
    }

    maxIdleConnectionsPerHost, err := bound.Resolve(config.MaxIdleConnectionsPerHost, maxIdleConnectionsPerHostRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.MaxIdleConnectionsPerHost = maxIdleConnectionsPerHost.Value
    if true == maxIdleConnectionsPerHost.Unlimited {
        /* net/http reads a zero per-host pool as its default of two, so unlimited is the largest int */
        resolved.MaxIdleConnectionsPerHost = math.MaxInt
    }

    /* the HTTP/2 transport reads DisableKeepAlives alone, never a negative per-host pool */
    resolved.DisableKeepAlives = config.IdleConnectionsDisabled

    idleConnectionTimeout, err := bound.Resolve(config.IdleConnectionTimeout, idleConnectionTimeoutRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    /* net/http reads a zero idle timeout as no limit, which is what unlimited resolves to */
    resolved.IdleConnectionTimeout = idleConnectionTimeout.Value

    tlsHandshakeTimeout, err := bound.Resolve(config.TlsHandshakeTimeout, tlsHandshakeTimeoutRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.TlsHandshakeTimeout = tlsHandshakeTimeout.Value

    expectContinueTimeout, err := bound.Resolve(config.ExpectContinueTimeout, expectContinueTimeoutRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.ExpectContinueTimeout = expectContinueTimeout.Value

    responseHeaderTimeout, err := bound.Resolve(config.ResponseHeaderTimeout, responseHeaderTimeoutRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.ResponseHeaderTimeout = responseHeaderTimeout.Value

    return resolved, nil
}
