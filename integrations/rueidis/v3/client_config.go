package rueidis

import (
    "crypto/tls"
    "time"
)

func DefaultClientConfig() *ClientConfig {
    return &ClientConfig{
        ClientName:       "",
        SelectDb:         0,
        DisableCache:     true,
        TlsConfig:        nil,
        PingOnStart:      true,
        DialTimeout:      5 * time.Second,
        ConnWriteTimeout: 5 * time.Second,
    }
}

/* ClientConfig has different zero-value behavior from DefaultClientConfig: PingOnStart and DisableCache are false in a zero value and true in the defaults. Start from DefaultClientConfig when adjusting selected fields; enabling client-side caching also enables its memory and protocol requirements. */
type ClientConfig struct {
    ClientName       string
    SelectDb         int
    DisableCache     bool
    TlsConfig        *tls.Config
    PingOnStart      bool
    DialTimeout      time.Duration
    ConnWriteTimeout time.Duration
}
