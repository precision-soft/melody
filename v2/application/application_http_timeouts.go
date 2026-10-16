package application

import (
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

/* the per-request server limits are fixed in this major; a slow client is cut instead of holding a connection open. The shutdown wait is configurable through MELODY_HTTP_SHUTDOWN_TIMEOUT, since its value belongs to the supervisor's termination grace. */
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

/* applyHttpServerErrorLog routes what net/http reports on its own, a connection failing before a request exists or a request rejected before any handler, into the application's journal instead of unstructured stderr. */
func applyHttpServerErrorLog(httpServer *nethttp.Server, logger loggingcontract.Logger) {
    httpServer.ErrorLog = logging.NewStandardErrorLogger(logger, "http server error")
}
