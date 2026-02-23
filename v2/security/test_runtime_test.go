package security

import (
    "context"
    "errors"
    "reflect"
    "sync"

    "github.com/precision-soft/melody/v2/clock"
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type testScope struct {
    mutex      sync.RWMutex
    valueByKey map[string]any
    closed     bool
}

func newTestScope() *testScope {
    return &testScope{
        valueByKey: make(map[string]any),
    }
}

func (instance *testScope) Get(serviceName string) (any, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if true == instance.closed {
        return nil, errors.New("scope is closed")
    }

    value, exists := instance.valueByKey[serviceName]
    if false == exists {
        return nil, errors.New("service not found")
    }

    return value, nil
}

func (instance *testScope) MustGet(serviceName string) any {
    value, err := instance.Get(serviceName)
    if nil != err {
        panic(err)
    }

    return value
}

func (instance *testScope) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *testScope) MustGetByType(targetType reflect.Type) any {
    panic(errors.New("not implemented"))
}

func (instance *testScope) Has(serviceName string) bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if true == instance.closed {
        return false
    }

    _, exists := instance.valueByKey[serviceName]
    return true == exists
}

func (instance *testScope) HasType(targetType reflect.Type) bool {
    return false
}

func (instance *testScope) OverrideInstance(serviceName string, value any) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return errors.New("scope is closed")
    }

    instance.valueByKey[serviceName] = value
    return nil
}

func (instance *testScope) MustOverrideInstance(serviceName string, value any) {
    err := instance.OverrideInstance(serviceName, value)
    if nil != err {
        panic(err)
    }
}

func (instance *testScope) OverrideProtectedInstance(serviceName string, value any) error {
    return instance.OverrideInstance(serviceName, value)
}

func (instance *testScope) MustOverrideProtectedInstance(serviceName string, value any) {
    err := instance.OverrideProtectedInstance(serviceName, value)
    if nil != err {
        panic(err)
    }
}

func (instance *testScope) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.closed = true
    return nil
}

var _ containercontract.Scope = (*testScope)(nil)

type testKernel struct {
    configuration    configcontract.Configuration
    serviceContainer containercontract.Container
    eventDispatcher  eventcontract.EventDispatcher
    httpKernel       httpcontract.Kernel
    httpRouter       httpcontract.Router
    clock            clockcontract.Clock
}

func newTestKernel() *testKernel {
    httpRouter := http.NewRouter()

    return &testKernel{
        configuration:    nil,
        serviceContainer: container.NewContainer(),
        eventDispatcher:  event.NewEventDispatcher(clock.NewSystemClock()),
        httpKernel:       http.NewKernel(httpRouter),
        httpRouter:       httpRouter,
        clock:            clock.NewSystemClock(),
    }
}

func (instance *testKernel) Environment() string {
    return "test"
}

func (instance *testKernel) DebugMode() bool { return true }

func (instance *testKernel) ServiceContainer() containercontract.Container {
    return nil
}

func (instance *testKernel) EventDispatcher() eventcontract.EventDispatcher {
    return instance.eventDispatcher
}

func (instance *testKernel) Config() configcontract.Configuration { return nil }

func (instance *testKernel) HttpKernel() httpcontract.Kernel {
    return instance.httpKernel
}

func (instance *testKernel) HttpRouter() httpcontract.Router {
    return instance.httpRouter
}

func (instance *testKernel) Clock() clockcontract.Clock { return nil }

var _ kernelcontract.Kernel = (*testKernel)(nil)

func newTestRuntime() runtimecontract.Runtime {
    scope := newTestScope()
    serviceContainer := container.NewContainer()

    overrideErr := scope.OverrideInstance(
        logging.ServiceLogger,
        logging.NewNopLogger(),
    )
    if nil != overrideErr {
        panic(overrideErr)
    }

    return runtime.New(context.Background(), scope, serviceContainer)
}
