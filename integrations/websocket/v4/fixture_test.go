package websocket

import (
    "time"

    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* newStreamHandlerPastTheDoor builds a handler pinging at an interval below the one-second minimum, so the ping and reap tests run in milliseconds; every other limit is resolved by its rule */
func newStreamHandlerPastTheDoor(hub *melodyhttp.ServerSentEventHub, options Options, idleTimeout time.Duration) httpcontract.Handler {
    resolved, resolveErr := resolveOptions(options)
    if nil != resolveErr {
        panic(resolveErr)
    }

    resolved.idleTimeout = idleTimeout

    return newStreamHandler(hub, resolved)
}
