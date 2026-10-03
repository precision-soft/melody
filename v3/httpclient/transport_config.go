package httpclient

import (
    "math"
    "time"
)

/* DefaultTransportConfig is the transport melody builds when no override names a field. Every field is populated, so it doubles as the statement of the defaults; overriding starts from a zero-valued TransportConfig, not from this. */
func DefaultTransportConfig() *TransportConfig {
    return &TransportConfig{
        DialTimeout:           TransportDuration(10 * time.Second),
        KeepAlive:             TransportDuration(30 * time.Second),
        MaxIdleConns:          TransportCount(100),
        MaxIdleConnsPerHost:   TransportCount(100),
        IdleConnTimeout:       TransportDuration(90 * time.Second),
        TlsHandshakeTimeout:   TransportDuration(10 * time.Second),
        ExpectContinueTimeout: TransportDuration(1 * time.Second),
        ResponseHeaderTimeout: TransportDuration(15 * time.Second),
    }
}

/* TransportDuration keeps a TransportConfig literal a literal: the fields are pointers so that unset and zero stay two different statements, and Go offers no address of a constant inline. */
func TransportDuration(value time.Duration) *time.Duration {
    return &value
}

/* TransportCount is TransportDuration for the connection-count fields. */
func TransportCount(value int) *int {
    return &value
}

/* TransportConfig overrides the transport melody builds for a client. A nil field falls back to its default in DefaultTransportConfig; a set field reaches net/http verbatim, zero and negative included, with the meaning net/http and net.Dialer give it. */
type TransportConfig struct {
    DialTimeout *time.Duration
    KeepAlive   *time.Duration

    MaxIdleConns *int

    /* MaxIdleConnsPerHost bounds the idle pool of one host; net/http's default of two would close every connection past the second as it idles, exhausting ephemeral ports under a burst. It defaults to MaxIdleConns and follows its meaning: an unbounded total makes the host unbounded, spelled as the largest count, and a negative total disables keep-alives. */
    MaxIdleConnsPerHost *int

    IdleConnTimeout       *time.Duration
    TlsHandshakeTimeout   *time.Duration
    ExpectContinueTimeout *time.Duration
    ResponseHeaderTimeout *time.Duration
}

/* resolvedTransportConfig is the concrete-value form the constructor reads; TransportConfig is the overlay over it, where nil means unset. */
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

func resolveTransportConfig(override *TransportConfig) resolvedTransportConfig {
    defaults := DefaultTransportConfig()

    resolved := resolvedTransportConfig{
        DialTimeout:           *defaults.DialTimeout,
        KeepAlive:             *defaults.KeepAlive,
        MaxIdleConns:          *defaults.MaxIdleConns,
        MaxIdleConnsPerHost:   *defaults.MaxIdleConnsPerHost,
        IdleConnTimeout:       *defaults.IdleConnTimeout,
        TlsHandshakeTimeout:   *defaults.TlsHandshakeTimeout,
        ExpectContinueTimeout: *defaults.ExpectContinueTimeout,
        ResponseHeaderTimeout: *defaults.ResponseHeaderTimeout,
    }

    if nil == override {
        return resolved
    }

    if nil != override.DialTimeout {
        resolved.DialTimeout = *override.DialTimeout
    }

    if nil != override.KeepAlive {
        resolved.KeepAlive = *override.KeepAlive
    }

    if nil != override.MaxIdleConns {
        resolved.MaxIdleConns = *override.MaxIdleConns

        /* the per-host pool follows the total's meaning unless the caller pins it: zero is an unbounded total, which copied to the host would read as net/http's default of two, so the host becomes the largest count */
        resolved.MaxIdleConnsPerHost = *override.MaxIdleConns
        if 0 == *override.MaxIdleConns {
            resolved.MaxIdleConnsPerHost = math.MaxInt
        }
    }

    if nil != override.MaxIdleConnsPerHost {
        resolved.MaxIdleConnsPerHost = *override.MaxIdleConnsPerHost
    }

    if nil != override.IdleConnTimeout {
        resolved.IdleConnTimeout = *override.IdleConnTimeout
    }

    if nil != override.TlsHandshakeTimeout {
        resolved.TlsHandshakeTimeout = *override.TlsHandshakeTimeout
    }

    if nil != override.ExpectContinueTimeout {
        resolved.ExpectContinueTimeout = *override.ExpectContinueTimeout
    }

    if nil != override.ResponseHeaderTimeout {
        resolved.ResponseHeaderTimeout = *override.ResponseHeaderTimeout
    }

    return resolved
}
