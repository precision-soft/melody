package websocket

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

const defaultStreamRouteName = "melody.websocket"

type ModuleConfig struct {
    Hub *melodyhttp.ServerSentEventHub
    /* Options is handed to NewStreamHandler untouched, so its IdleTimeout requirement is the module's too: a zero fails the route registration at boot. The module deliberately supplies no default of its own — the only thing that reaps a peer which vanished without a fin should be chosen by the application, not inherited silently. */
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
    return "registers the websocket stream route bridged onto a server-sent-event hub"
}

/* a missing hub or path is refused at boot rather than skipped: an unregistered route has no later consumer to fail loudly — the endpoint simply does not exist, clients get 404 and every boot-time check reads healthy — while the same module already panics one field over on a zero IdleTimeout. A module registered at all is a decision to serve the stream. Only the PATH carries its own guard: a nil hub is already refused by name inside NewStreamHandler, which HandleNamed below reaches at this same boot moment, so a second check in front of it would be a shadowed sister no test could pin. */
func (instance *Module) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    if "" == instance.config.Path {
        exception.Panic(exception.NewError("websocket module path is empty - typically a missing configuration key; the stream route cannot be registered without one", nil, nil))
    }

    routeName := instance.config.RouteName
    if "" == routeName {
        routeName = defaultStreamRouteName
    }

    kernelInstance.HttpRouter().HandleNamed(
        routeName,
        "GET",
        instance.config.Path,
        NewStreamHandler(instance.config.Hub, instance.config.Options),
    )
}

var (
    _ applicationcontract.Module     = (*Module)(nil)
    _ applicationcontract.HttpModule = (*Module)(nil)
)
