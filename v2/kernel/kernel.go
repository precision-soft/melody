package kernel

import (
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

func NewKernel(
    applicationConfiguration configcontract.Configuration,
    serviceContainer containercontract.Container,
    httpRouter httpcontract.Router,
    eventDispatcher eventcontract.EventDispatcher,
    clock clockcontract.Clock,
) kernelcontract.Kernel {
    if nil == applicationConfiguration {
        exception.Panic(
            exception.NewError("application configuration is required for new kernel", nil, nil),
        )
    }

    if nil == serviceContainer {
        exception.Panic(
            exception.NewError("service container is required for new kernel", nil, nil),
        )
    }

    if nil == httpRouter {
        exception.Panic(
            exception.NewError("http router is required for new kernel", nil, nil),
        )
    }

    if nil == eventDispatcher {
        exception.Panic(
            exception.NewError("event dispatcher is required for new kernel", nil, nil),
        )
    }

    if nil == clock {
        exception.Panic(
            exception.NewError("clock is required for new kernel", nil, nil),
        )
    }

    httpKernel := http.NewKernel(httpRouter)

    return &kernel{
        config:           applicationConfiguration,
        serviceContainer: serviceContainer,
        httpRouter:       httpRouter,
        httpKernel:       httpKernel,
        eventDispatcher:  eventDispatcher,
        clock:            clock,
    }
}

type kernel struct {
    config           configcontract.Configuration
    serviceContainer containercontract.Container
    httpRouter       httpcontract.Router
    httpKernel       httpcontract.Kernel
    eventDispatcher  eventcontract.EventDispatcher
    clock            clockcontract.Clock
}

func (instance *kernel) Environment() string {
    return instance.config.Kernel().Env()
}

func (instance *kernel) DebugMode() bool {
    return config.EnvDevelopment == instance.config.Kernel().Env()
}

func (instance *kernel) ServiceContainer() containercontract.Container {
    return instance.serviceContainer
}

func (instance *kernel) EventDispatcher() eventcontract.EventDispatcher {
    return instance.eventDispatcher
}

func (instance *kernel) Config() configcontract.Configuration {
    return instance.config
}

func (instance *kernel) HttpRouter() httpcontract.Router {
    return instance.httpRouter
}

func (instance *kernel) HttpKernel() httpcontract.Kernel {
    return instance.httpKernel
}

func (instance *kernel) Clock() clockcontract.Clock {
    return instance.clock
}

var _ kernelcontract.Kernel = (*kernel)(nil)
