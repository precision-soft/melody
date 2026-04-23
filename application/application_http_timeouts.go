package application

import (
    nethttp "net/http"
    "time"
)

type HttpTimeoutConfiguration interface {
    GetReadTimeout() time.Duration

    GetReadHeaderTimeout() time.Duration

    GetWriteTimeout() time.Duration

    GetIdleTimeout() time.Duration

    GetMaxHeaderBytes() int
}

const (
    defaultHttpReadTimeout       = 15 * time.Second
    defaultHttpReadHeaderTimeout = 5 * time.Second
    defaultHttpWriteTimeout      = 30 * time.Second
    defaultHttpIdleTimeout       = 60 * time.Second
    defaultHttpMaxHeaderBytes    = 1 << 20
)

func applyHttpServerTimeouts(httpServer *nethttp.Server, configuration any) {
    httpServer.ReadTimeout = defaultHttpReadTimeout
    httpServer.ReadHeaderTimeout = defaultHttpReadHeaderTimeout
    httpServer.WriteTimeout = defaultHttpWriteTimeout
    httpServer.IdleTimeout = defaultHttpIdleTimeout
    httpServer.MaxHeaderBytes = defaultHttpMaxHeaderBytes

    overrides, ok := configuration.(HttpTimeoutConfiguration)
    if false == ok {
        return
    }

    httpServer.ReadTimeout = overrides.GetReadTimeout()
    httpServer.ReadHeaderTimeout = overrides.GetReadHeaderTimeout()
    httpServer.WriteTimeout = overrides.GetWriteTimeout()
    httpServer.IdleTimeout = overrides.GetIdleTimeout()
    httpServer.MaxHeaderBytes = overrides.GetMaxHeaderBytes()
}
