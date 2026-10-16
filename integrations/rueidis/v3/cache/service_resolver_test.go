package cache

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/redis/rueidis"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodycache "github.com/precision-soft/melody/v3/cache"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

type recordingRegistrar struct {
    names []string
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func TestRegisterBackendServiceUsesCoreBackendName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterBackendService(registrar, nil, "example")

    if 0 == len(registrar.names) || melodycache.ServiceCacheBackend != registrar.names[0] {
        t.Fatalf("expected %q to be registered, got %v", melodycache.ServiceCacheBackend, registrar.names)
    }
}

type teardownSpyClient struct {
    rueidis.Client
    closed bool
}

func (instance *teardownSpyClient) Close() {
    instance.closed = true
}

type containerRegistrar struct {
    target containercontract.Container
}

func (instance *containerRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.target.MustRegister(serviceName, provider, options...)
}

func (instance *containerRegistrar) Register(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    return instance.target.Register(serviceName, provider, options...)
}

func (instance *containerRegistrar) MustRegister(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.target.MustRegister(serviceName, provider, options...)
}

/* the backend borrows the client and declines to close it; resolving it must record the edge that lets the teardown close the owning connection after it */
func TestRegisterBackendService_ResolutionOrdersTheConnectionIntoTheTeardown(t *testing.T) {
    client := &teardownSpyClient{}
    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}

    melodyrueidis.RegisterConnectionService(registrar, melodyrueidis.NewConnection(client))
    RegisterBackendService(registrar, client, "prefix")

    backend := container.MustFromResolver[cachecontract.Backend](serviceContainer, melodycache.ServiceCacheBackend)
    if nil == backend {
        t.Fatalf("expected the backend to resolve")
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("container close: %v", closeErr)
    }

    if false == client.closed {
        t.Fatalf("expected the resolved backend to pull the owning connection into the teardown")
    }
}

/* the registered backend is probed over a store whose replies stop arriving, under a timer far below the client's five-second ceiling: a Get the client retries for as long as the context allows fails on the timer, a bounded one is refused with its own deadline */
const registeredBackendProbeBudget = 2 * time.Second

func TestRegisterBackendServiceWithOptions_HandsTheOptionsToTheBackend(t *testing.T) {
    client, backendGate := dialGated(t)

    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}
    RegisterBackendServiceWithOptions(registrar, client, "melody:test:"+t.Name()+":", WithCommandTimeout(50*time.Millisecond), WithMaxKeyLength(7))

    service := container.MustFromResolver[*BackendService](serviceContainer, melodycache.ServiceCacheBackend)

    if 50*time.Millisecond != service.backend.commandTimeout || 7 != service.backend.maxKeyLength {
        t.Fatalf("expected the options to reach the registered backend, got timeout %v and max key length %d", service.backend.commandTimeout, service.backend.maxKeyLength)
    }

    _ = service.Set("warm", []byte("x"), 0)
    _ = service.Delete("warm")

    backendGate.Wedge()

    getErr := awaitOutcome(t, registeredBackendProbeBudget, func() error {
        _, _, err := service.Get("key")

        return err
    })
    if nil == getErr || false == errors.Is(getErr, context.DeadlineExceeded) {
        t.Fatalf("expected the registered backend's read to be refused with its own deadline, got %v", getErr)
    }
}

/* PIN of the subpackage's convention, not a guard: the door without options registers the backend unbounded, the behaviour of a backend built without WithCommandTimeout */
func TestRegisterBackendService_KeepsTheUnboundedDefault(t *testing.T) {
    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}
    RegisterBackendService(registrar, &teardownSpyClient{}, "prefix")

    service := container.MustFromResolver[*BackendService](serviceContainer, melodycache.ServiceCacheBackend)

    if 0 != service.backend.commandTimeout {
        t.Fatalf("expected the option-less registration to stay unbounded, got %v", service.backend.commandTimeout)
    }
}
