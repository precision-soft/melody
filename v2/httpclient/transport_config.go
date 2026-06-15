package httpclient

import "time"

func DefaultTransportConfig() *TransportConfig {
    return &TransportConfig{
        DialTimeout:           10 * time.Second,
        KeepAlive:             30 * time.Second,
        MaxIdleConns:          100,
        IdleConnTimeout:       90 * time.Second,
        TlsHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 15 * time.Second,
    }
}

type TransportConfig struct {
    DialTimeout           time.Duration
    KeepAlive             time.Duration
    MaxIdleConns          int
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
