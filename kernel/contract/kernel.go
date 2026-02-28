package contract

import (
    clockcontract "github.com/precision-soft/melody/clock/contract"
    configcontract "github.com/precision-soft/melody/config/contract"
    containercontract "github.com/precision-soft/melody/container/contract"
    eventcontract "github.com/precision-soft/melody/event/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
)

type Kernel interface {
    Environment() string

    DebugMode() bool

    ServiceContainer() containercontract.Container

    EventDispatcher() eventcontract.EventDispatcher

    Config() configcontract.Configuration

    HttpRouter() httpcontract.Router

    HttpKernel() httpcontract.Kernel

    Clock() clockcontract.Clock
}
