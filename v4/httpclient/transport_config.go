package httpclient

import "time"

/* DefaultTransportConfig is the transport melody builds when no override names a field. Every field is set, so it doubles as the statement of the defaults; overriding starts from a zero-valued TransportConfig, not from this. */
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

/* TransportDuration answers a pointer to the duration, for a TransportConfig field. */
func TransportDuration(value time.Duration) *time.Duration {
    return &value
}

/* TransportCount answers a pointer to the count, for a TransportConfig field. */
func TransportCount(value int) *int {
    return &value
}

/* TransportConfig overrides the transport melody builds for a client. A nil field takes its default; a set field reaches net/http as it is, zero and negative included, so the meanings net/http and net.Dialer give them are reachable: a zero or negative timeout is no deadline, a zero MaxIdleConns an unbounded pool, a negative MaxIdleConns or MaxIdleConnsPerHost keeps no idle connection, a negative KeepAlive turns keep-alive off. Build the fields with TransportDuration and TransportCount. */
type TransportConfig struct {
    DialTimeout *time.Duration
    KeepAlive   *time.Duration

    MaxIdleConns *int

    /* MaxIdleConnsPerHost bounds the idle pool of one host. Left nil it follows a positive MaxIdleConns and otherwise keeps its default; set to zero it is net/http's default of two, which closes every connection past the second as it idles and exhausts ephemeral ports under a burst. */
    MaxIdleConnsPerHost *int

    IdleConnTimeout       *time.Duration
    TlsHandshakeTimeout   *time.Duration
    ExpectContinueTimeout *time.Duration
    ResponseHeaderTimeout *time.Duration
}

/* resolvedTransportConfig is the transport configuration with every field decided, as the client hands it to net/http. */
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

        /* the per-host pool follows a bounded total unless the caller sets it, so raising MaxIdleConns alone is not capped at net/http's per-host default of two; a total of zero or below leaves it at its default, since net/http reads a zero host pool as two and a negative one as none */
        if 0 < resolved.MaxIdleConns {
            resolved.MaxIdleConnsPerHost = resolved.MaxIdleConns
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
