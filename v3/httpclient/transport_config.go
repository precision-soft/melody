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

/* TransportConfig overrides the transport melody builds for a client. A nil field means "not set" and falls back to the default beside it in DefaultTransportConfig; a set field reaches net/http VERBATIM, zero and negative included, carrying the meaning net/http and net.Dialer document for it — MaxIdleConns zero is an unbounded pool, IdleConnTimeout or ResponseHeaderTimeout zero waits forever, a negative KeepAlive disables the probes. TransportDuration and TransportCount build the pointers in place. */
type TransportConfig struct {
    DialTimeout *time.Duration
    KeepAlive   *time.Duration

    MaxIdleConns *int

    /* MaxIdleConnsPerHost bounds the idle pool of a single host. net/http defaults it to two, which caps the whole pool for a client bound to one BaseUrl: every connection past the second is closed as soon as it goes idle, so a burst dials as many sockets as it has requests and leaves almost all of them in TIME_WAIT for the MSL, until the ephemeral port range runs out and every request fails to connect. It defaults to MaxIdleConns and follows an override of it, in its meaning: an unbounded total (zero) makes the host unbounded too, spelled as the largest count because net/http reads a per-host zero as its default of two, and a negative total makes the host negative, which net/http reads as keep-alives disabled — every request dials. */
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

        /* the per-host pool follows the total unless the caller pins it, so raising MaxIdleConns alone is never silently capped at net/http's per-host default of two; it follows the total's meaning rather than its number — zero is an unbounded total, and copied to the host it would read as net/http's default of two, the very cap this rule exists to avoid, so the host becomes unbounded in the only spelling net/http has for it */
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
