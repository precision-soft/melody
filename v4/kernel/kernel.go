package kernel

import (
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/kernelseam"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

/* NewKernel is called by the application alone: a token NewToken did not build is refused. The http kernel is built from httpKernelConfig, its one source. */
func NewKernel(
    token kernelseam.Token,
    applicationConfiguration configcontract.Configuration,
    serviceContainer containercontract.Container,
    httpRouter httpcontract.Router,
    eventDispatcher eventcontract.EventDispatcher,
    clock clockcontract.Clock,
    httpKernelConfig http.KernelConfig,
) kernelcontract.Kernel {
    if false == token.Valid() {
        exception.Panic(
            exception.NewError("NewKernel is called by the framework only", nil, nil),
        )
    }

    if true == internal.IsNilInterface(applicationConfiguration) {
        exception.Panic(
            exception.NewError("application configuration is required for new kernel", nil, nil),
        )
    }

    if true == internal.IsNilInterface(serviceContainer) {
        exception.Panic(
            exception.NewError("service container is required for new kernel", nil, nil),
        )
    }

    if true == internal.IsNilInterface(httpRouter) {
        exception.Panic(
            exception.NewError("http router is required for new kernel", nil, nil),
        )
    }

    if true == internal.IsNilInterface(eventDispatcher) {
        exception.Panic(
            exception.NewError("event dispatcher is required for new kernel", nil, nil),
        )
    }

    if true == internal.IsNilInterface(clock) {
        exception.Panic(
            exception.NewError("clock is required for new kernel", nil, nil),
        )
    }

    httpKernel := http.NewKernel(httpRouter, httpKernelConfig)

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
    bootRuntime      containercontract.Runtime
    config           configcontract.Configuration
    serviceContainer containercontract.Container
    httpRouter       httpcontract.Router
    httpKernel       httpcontract.Kernel
    eventDispatcher  eventcontract.EventDispatcher
    clock            clockcontract.Clock
}

func (instance *kernel) BootRuntime() containercontract.Runtime {
    return instance.bootRuntime
}

func (instance *kernel) InstallBootRuntime(token kernelseam.Token, bootRuntime containercontract.Runtime) {
    if false == token.Valid() {
        exception.Panic(
            exception.NewError("kernel InstallBootRuntime is called by the framework only", nil, nil),
        )
    }

    instance.bootRuntime = bootRuntime
}

var _ kernelseam.BootRuntimeInstaller = (*kernel)(nil)

func (instance *kernel) Environment() string {
    return instance.config.Kernel().Environment()
}

func (instance *kernel) DebugMode() bool {
    return config.EnvironmentDevelopment == instance.config.Kernel().Environment()
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
