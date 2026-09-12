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

/* ClientConfig's zero value is NOT DefaultClientConfig: the documented defaults are PingOnStart true and DisableCache true, while a Go struct starts both at false — so a partial literal written to tune one field silently disarms the boot ping and turns client-side caching ON, a subsystem with its own memory budget (128 MiB per connection at the library default) and RESP3 requirements. Start from DefaultClientConfig() and adjust fields, never from &ClientConfig{}. The blast radius is bounded either way, measured: the library normalizes a zero dial timeout to its own five-second default, and a dead address still refuses eagerly at client creation even with the ping off. */
type ClientConfig struct {
    ClientName       string
    SelectDb         int
    DisableCache     bool
    TlsConfig        *tls.Config
    PingOnStart      bool
    DialTimeout      time.Duration
    ConnWriteTimeout time.Duration
}
