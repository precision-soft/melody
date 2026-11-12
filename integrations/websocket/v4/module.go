package websocket

import (
    "fmt"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
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
    Hub *melodyhttp.ServerSentEventHub
    /* Options is handed to NewStreamHandler untouched, so a limit outside its rule fails the route registration at boot. */
    Options   Options
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

/* every stream is judged and its handler built before the first route registers, so a refused module leaves no route behind; a missing stream list, hub, path or route name is refused at boot rather than skipped, since an unregistered route has no later consumer to fail loudly, and an option outside its rule is refused by NewStreamHandler naming the option */
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
                "websocket module hub is nil; the stream route cannot be registered without one",
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

        handlers = append(handlers, NewStreamHandler(stream.Hub, stream.Options))
    }

    for index, stream := range streams {
        routeName := stream.RouteName
        if "" == routeName {
            routeName = defaultStreamRouteName
        }

        kernelInstance.HttpRouter().HandleWithOptions(
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
