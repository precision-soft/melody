package contract

import (
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type Kernel interface {
    /* BootRuntime is the runtime of the application's boot, the one a boot hook resolves and calls its doors under; once Boot returned its context is done with containercontract.ErrBootFinished as its cause and its scope is closed. It is nil before Boot. */
    BootRuntime() containercontract.Runtime

    Environment() string

    DebugMode() bool

    ServiceContainer() containercontract.Container

    EventDispatcher() eventcontract.EventDispatcher

    Config() configcontract.Configuration

    HttpRouter() httpcontract.Router

    Clock() clockcontract.Clock
}
