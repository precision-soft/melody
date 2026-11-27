package websocket

import (
    "fmt"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

const defaultStreamRouteName = "melody.websocket"

/* ModuleConfig declares every stream route of the application, since a module registers once per application: one entry per endpoint, each bridged onto its own hub. */
type ModuleConfig struct {
    Streams []StreamConfig
}

/* StreamConfig is one stream route. RouteName defaults to melody.websocket when the module declares a single stream and is required when it declares more, since one default cannot name two routes. */
type StreamConfig struct {
    /* Hub answers the hub of each connection through the connection's runtime, so a hub its container provider builds at boot is reached after the routes register; its error is the request's, a hub it answers nil is refused. */
    Hub func(runtimeInstance containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error)
    /* Handler is resolved once, at the route registration, so a limit outside its rule fails the boot. */
    Handler   StreamHandlerConfig
    RouteName string
    Path      string
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "websocket"
}

func (instance *Module) Description() string {
    return "registers the websocket stream routes, each bridged onto a server-sent-event hub"
}

/* every stream is judged and its handler built before the first route registers, so a refused module leaves no route behind; a missing stream list, hub provider, path or route name is refused at boot rather than skipped, since an unregistered route has no later consumer to fail loudly, and a handler limit outside its rule is refused by NewStreamHandler naming the field */
func (instance *Module) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    streams := instance.config.Streams
    if 0 == len(streams) {
        exception.Panic(exception.NewError(
            "websocket module declares no stream; the module registers nothing without one",
            map[string]any{"field": "ModuleConfig.Streams"},
            nil,
        ))
    }

    handlers := make([]httpcontract.Handler, 0, len(streams))
    for index, stream := range streams {
        if nil == stream.Hub {
            exception.Panic(exception.NewError(
                "websocket module hub provider is nil; the stream route cannot be registered without one",
                map[string]any{"field": fmt.Sprintf("ModuleConfig.Streams[%d].Hub", index)},
                nil,
            ))
        }

        if "" == stream.Path {
            exception.Panic(exception.NewError(
                "websocket module path is empty - typically a missing configuration key; the stream route cannot be registered without one",
                map[string]any{"field": fmt.Sprintf("ModuleConfig.Streams[%d].Path", index)},
                nil,
            ))
        }

        if "" == stream.RouteName && 1 < len(streams) {
            exception.Panic(exception.NewError(
                "websocket module route name is empty on a module declaring more than one stream; the default names a single route",
                map[string]any{"field": fmt.Sprintf("ModuleConfig.Streams[%d].RouteName", index)},
                nil,
            ))
        }

        resolved, resolveErr := resolveStreamHandlerConfig(stream.Handler)
        if nil != resolveErr {
            exception.Panic(exception.FromError(resolveErr))
        }

        handlers = append(handlers, newStreamHandler(stream.Hub, resolved))
    }

    for index, stream := range streams {
        routeName := stream.RouteName
        if "" == routeName {
            routeName = defaultStreamRouteName
        }

        kernelInstance.HttpRouter().Handle(
            stream.Path,
            handlers[index],
            melodyhttp.RouteOptions{Name: routeName, Methods: []string{"GET"}},
        )
    }
}

var (
    _ applicationcontract.Module     = (*Module)(nil)
    _ applicationcontract.HttpModule = (*Module)(nil)
)
