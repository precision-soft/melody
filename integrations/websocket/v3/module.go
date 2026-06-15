package websocket

import (
    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

const defaultStreamRouteName = "melody.websocket"

type ModuleConfig struct {
    Hub       *melodyhttp.ServerSentEventHub
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

func (instance *Module) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    if nil == instance.config.Hub || "" == instance.config.Path {
        return
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
