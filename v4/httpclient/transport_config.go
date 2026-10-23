package httpclient

import (
    "math"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* TransportConfig overrides the transport melody builds for a client. A field left out takes its default; a value below the field's minimum is refused, naming the field, and so is unlimited on a field that does not admit it. Every timeout but IdleConnTimeout refuses unlimited: the dial, TLS handshake, response header and 100-continue waits each guard against a peer that never answers, and KeepAlive has KeepAliveDisabled. */
type TransportConfig struct {
    /* DialTimeout bounds one dial: 10s by default, at least 1ms. */
    DialTimeout bound.Duration

    /* KeepAlive is the interval of the TCP keep-alive probes: 30s by default, at least 1s. KeepAliveDisabled turns the probes off and refuses a KeepAlive set beside it. */
    KeepAlive         bound.Duration
    KeepAliveDisabled bool

    /* MaxIdleConns bounds the idle pool across hosts: 100 by default, at least 1, unlimited admitted. */
    MaxIdleConns bound.Count

    /* MaxIdleConnsPerHost bounds the idle pool of one host: at least 1, unlimited admitted. Left out it follows a MaxIdleConns set to a value and otherwise is 100, so raising the total alone is not capped at net/http's per-host default of two. */
    MaxIdleConnsPerHost bound.Count

    /* IdleConnTimeout closes a connection idle that long: 90s by default, at least 1s, unlimited admitted. */
    IdleConnTimeout bound.Duration

    /* TlsHandshakeTimeout bounds the TLS handshake: 10s by default, at least 1ms. */
    TlsHandshakeTimeout bound.Duration

    /* ExpectContinueTimeout bounds the wait for a 100-continue: 1s by default; zero sends the body at once. */
    ExpectContinueTimeout bound.Duration

    /* ResponseHeaderTimeout bounds the wait for the response headers once the request is written: 15s by default, at least 1ms. */
    ResponseHeaderTimeout bound.Duration
}

/* resolvedTransportConfig is the transport configuration with every field decided, in net/http's terms. */
type resolvedTransportConfig struct {
    DialTimeout           time.Duration
    KeepAlive             time.Duration
    MaxIdleConns          int
    MaxIdleConnsPerHost   int
    IdleConnTimeout       time.Duration
    TlsHandshakeTimeout   time.Duration
    ExpectContinueTimeout time.Duration
    ResponseHeaderTimeout time.Duration
}

const defaultMaxIdleConns = 100

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

    maxIdleConnsRule = bound.Rule[int]{
        Name:              "TransportConfig.MaxIdleConns",
        Default:           defaultMaxIdleConns,
        Minimum:           1,
        UnlimitedAdmitted: true,
    }

    maxIdleConnsPerHostRule = bound.Rule[int]{
        Name:              "TransportConfig.MaxIdleConnsPerHost",
        Default:           defaultMaxIdleConns,
        Minimum:           1,
        UnlimitedAdmitted: true,
    }

    idleConnTimeoutRule = bound.Rule[time.Duration]{
        Name:              "TransportConfig.IdleConnTimeout",
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

    maxIdleConns, err := bound.Resolve(config.MaxIdleConns, maxIdleConnsRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.MaxIdleConns = maxIdleConns.Value
    if true == maxIdleConns.Unlimited {
        /* net/http reads a zero total as no limit */
        resolved.MaxIdleConns = 0
    }

    perHostRule := maxIdleConnsPerHostRule
    if _, set := config.MaxIdleConns.Value(); true == set {
        perHostRule.Default = maxIdleConns.Value
    }

    maxIdleConnsPerHost, err := bound.Resolve(config.MaxIdleConnsPerHost, perHostRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    resolved.MaxIdleConnsPerHost = maxIdleConnsPerHost.Value
    if true == maxIdleConnsPerHost.Unlimited {
        /* net/http reads a zero per-host pool as its default of two, so unlimited is the largest int */
        resolved.MaxIdleConnsPerHost = math.MaxInt
    }

    idleConnTimeout, err := bound.Resolve(config.IdleConnTimeout, idleConnTimeoutRule)
    if nil != err {
        return resolvedTransportConfig{}, err
    }
    /* net/http reads a zero idle timeout as no limit, which is what unlimited resolves to */
    resolved.IdleConnTimeout = idleConnTimeout.Value

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
