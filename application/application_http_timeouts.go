package application

import (
    nethttp "net/http"
    "time"
)

/* the per-request server limits are fixed in this major: nothing implements an override and nothing can inject one — the configuration the application consults is always the one it built itself. The values bound every request the server admits; a slow client is cut instead of holding a connection open forever. The shutdown wait is the one limit that is configurable, through MELODY_HTTP_SHUTDOWN_TIMEOUT, because its right value belongs to the supervisor's termination grace rather than to the framework. */
const (
    defaultHttpReadTimeout       = 15 * time.Second
    defaultHttpReadHeaderTimeout = 5 * time.Second
    defaultHttpWriteTimeout      = 30 * time.Second
    defaultHttpIdleTimeout       = 60 * time.Second
    defaultHttpMaxHeaderBytes    = 1 << 20
)

func applyHttpServerTimeouts(httpServer *nethttp.Server) {
    httpServer.ReadTimeout = defaultHttpReadTimeout
    httpServer.ReadHeaderTimeout = defaultHttpReadHeaderTimeout
    httpServer.WriteTimeout = defaultHttpWriteTimeout
    httpServer.IdleTimeout = defaultHttpIdleTimeout
    httpServer.MaxHeaderBytes = defaultHttpMaxHeaderBytes
}
