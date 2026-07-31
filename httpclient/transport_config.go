package httpclient

import "time"

func DefaultTransportConfig() *TransportConfig {
    return &TransportConfig{
        DialTimeout:           10 * time.Second,
        KeepAlive:             30 * time.Second,
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   100,
        IdleConnTimeout:       90 * time.Second,
        TlsHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 15 * time.Second,
    }
}

/* TransportConfig overrides the transport melody builds for a client. Every field reads a non-positive value as "not set" and falls back to the default beside it, so the meanings net/http gives to zero — MaxIdleConns zero for an unbounded pool, IdleConnTimeout or ResponseHeaderTimeout zero for no deadline — and the meaning net.Dialer gives to a negative KeepAlive cannot be reached through this type. A deployment that needs one of them asks for a duration large enough to be the same thing in practice. */
type TransportConfig struct {
    DialTimeout time.Duration
    KeepAlive   time.Duration

    MaxIdleConns int

    /* MaxIdleConnsPerHost bounds the idle pool of a single host. net/http defaults it to two, which caps the whole pool for a client bound to one BaseUrl: every connection past the second is closed as soon as it goes idle, so a burst dials as many sockets as it has requests and leaves almost all of them in TIME_WAIT for the MSL, until the ephemeral port range runs out and every request fails to connect. It defaults to MaxIdleConns and follows an override of it. */
    MaxIdleConnsPerHost int

    IdleConnTimeout       time.Duration
    TlsHandshakeTimeout   time.Duration
    ExpectContinueTimeout time.Duration
    ResponseHeaderTimeout time.Duration
}

func resolveTransportConfig(override *TransportConfig) TransportConfig {
    resolved := *DefaultTransportConfig()

    if nil == override {
        return resolved
    }

    if 0 < override.DialTimeout {
        resolved.DialTimeout = override.DialTimeout
    }

    if 0 < override.KeepAlive {
        resolved.KeepAlive = override.KeepAlive
    }

    if 0 < override.MaxIdleConns {
        resolved.MaxIdleConns = override.MaxIdleConns

        /* the per-host pool follows the total unless the caller pins it, so raising MaxIdleConns alone is never silently capped at net/http's per-host default of two */
        resolved.MaxIdleConnsPerHost = override.MaxIdleConns
    }

    if 0 < override.MaxIdleConnsPerHost {
        resolved.MaxIdleConnsPerHost = override.MaxIdleConnsPerHost
    }

    if 0 < override.IdleConnTimeout {
        resolved.IdleConnTimeout = override.IdleConnTimeout
    }

    if 0 < override.TlsHandshakeTimeout {
        resolved.TlsHandshakeTimeout = override.TlsHandshakeTimeout
    }

    if 0 < override.ExpectContinueTimeout {
        resolved.ExpectContinueTimeout = override.ExpectContinueTimeout
    }

    if 0 < override.ResponseHeaderTimeout {
        resolved.ResponseHeaderTimeout = override.ResponseHeaderTimeout
    }

    return resolved
}
