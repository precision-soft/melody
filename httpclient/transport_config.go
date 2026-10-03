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

/* TransportConfig overrides the transport melody builds for a client. Every field reads a non-positive value as not set and falls back to its default, so net/http's meanings for zero, an unbounded pool or no deadline, and net.Dialer's negative KeepAlive cannot be reached through this type. */
type TransportConfig struct {
    DialTimeout time.Duration
    KeepAlive   time.Duration

    MaxIdleConns int

    /* MaxIdleConnsPerHost bounds the idle pool of one host; net/http's default of two would close every connection past the second as it idles, exhausting ephemeral ports under a burst. It defaults to MaxIdleConns and follows an override of it. */
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

        /* the per-host pool follows the total unless the caller pins it, so raising MaxIdleConns alone is not capped at net/http's per-host default of two */
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
