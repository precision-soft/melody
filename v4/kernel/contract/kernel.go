package contract

import (
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
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
