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

/* ClientConfig's zero value is not DefaultClientConfig: the defaults are PingOnStart true and DisableCache true, while a struct literal starts both at false, so a partial literal disarms the boot ping and turns client-side caching on, with its own memory budget (128 MiB per connection at the library default) and RESP3 requirements. Start from DefaultClientConfig() and adjust fields. A zero dial timeout still takes the library's five-second default, and a dead address still refuses at client creation with the ping off. */
type ClientConfig struct {
    ClientName       string
    SelectDb         int
    DisableCache     bool
    TlsConfig        *tls.Config
    PingOnStart      bool
    DialTimeout      time.Duration
    ConnWriteTimeout time.Duration
}
