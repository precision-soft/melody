package websocket

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func broadcastTestRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}

/* pings below the one-second minimum, so the ping and reap tests run in milliseconds */
func newStreamHandlerPastTheDoor(hub *melodyhttp.ServerSentEventHub, config StreamHandlerConfig, idleTimeout time.Duration) httpcontract.Handler {
    resolved, resolveErr := resolveStreamHandlerConfig(config)
    if nil != resolveErr {
        panic(resolveErr)
    }

    resolved.idleTimeout = idleTimeout

    return newStreamHandler(fixedHub(hub), resolved)
}

func fixedHub(hub *melodyhttp.ServerSentEventHub) func(containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error) {
    return func(containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error) {
        return hub, nil
    }
}
