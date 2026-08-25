package cache

import (
    "testing"

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
