package websocket

import (
    "time"

    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* pings below the one-second minimum, so the ping and reap tests run in milliseconds */
func newStreamHandlerPastTheDoor(hub *melodyhttp.ServerSentEventHub, config StreamHandlerConfig, idleTimeout time.Duration) httpcontract.Handler {
    resolved, resolveErr := resolveStreamHandlerConfig(config)
    if nil != resolveErr {
        panic(resolveErr)
    }

    resolved.idleTimeout = idleTimeout

    return newStreamHandler(hub, resolved)
}
