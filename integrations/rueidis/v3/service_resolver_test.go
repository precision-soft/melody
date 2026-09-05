package rueidis

import (
    "testing"
    "time"

    "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
)

type recordingRegistrar struct {
    names []string
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func (instance *recordingRegistrar) has(serviceName string) bool {
    for _, name := range instance.names {
        if serviceName == name {
            return true
        }
    }

    return false
}

func TestRegisterClientServiceUsesClientName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterClientService(registrar, nil)

    if false == registrar.has(ServiceClient) {
        t.Fatalf("expected %q to be registered, got %v", ServiceClient, registrar.names)
    }
}

func TestRegisterLockerServiceUsesCoreLockerName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterLockerService(registrar, nil)

    if false == registrar.has(melodylock.ServiceLocker) {
        t.Fatalf("expected %q to be registered, got %v", melodylock.ServiceLocker, registrar.names)
    }
}

func TestRegisterTokenStoreServiceUsesTokenStoreName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterTokenStoreService(registrar, nil)

    if false == registrar.has(ServiceTokenStore) {
        t.Fatalf("expected %q to be registered, got %v", ServiceTokenStore, registrar.names)
    }
}

type closeSpyClient struct {
    rueidis.Client
    closed bool
}

func (instance *closeSpyClient) Close() {
    instance.closed = true
}

/* containerRegistrar adapts the raw container to this package's registrar interface, so the teardown proof runs against the real ordered close rather than a spy */
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

/* E6-15: the raw client's Close returns nothing, so registered alone it could never join the container's teardown and the connection lived exactly as long as the process; through the owning Connection and the dependency edge, whichever run resolves a client-backed service closes the connection after it */
func TestRegisterConnectionService_TeardownClosesTheClientOnceTheClientWasResolved(t *testing.T) {
    client := &closeSpyClient{}
    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}

    RegisterConnectionService(registrar, NewConnection(client))
    RegisterClientService(registrar, client)

    resolvedClient := ClientMustFromContainer(serviceContainer)
    if nil == resolvedClient {
        t.Fatalf("expected the client to resolve")
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("container close: %v", closeErr)
    }

    if false == client.closed {
        t.Fatalf("expected the teardown to close the client through the owning connection")
    }
}

/* the guarantee's declared boundary: a run that resolves no client-backed service leaves the connection unresolved, and the container closes only what was resolved at least once */
func TestRegisterConnectionService_TeardownLeavesAnUnresolvedConnectionOpen(t *testing.T) {
    client := &closeSpyClient{}
    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}

    RegisterConnectionService(registrar, NewConnection(client))
    RegisterClientService(registrar, client)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("container close: %v", closeErr)
    }

    if true == client.closed {
        t.Fatalf("expected an unresolved connection to stay the composition root's to close")
    }
}

func TestRegisterLockerServiceWithOptions_HandsTheOptionsToTheLocker(t *testing.T) {
    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}

    RegisterLockerServiceWithOptions(registrar, fakeClient{}, WithLockerCallTimeout(750*time.Millisecond))

    locker, ok := melodylock.LockerMustFromContainer(serviceContainer).(*Locker)
    if false == ok {
        t.Fatalf("expected the registered locker to be this package's, got %T", melodylock.LockerMustFromContainer(serviceContainer))
    }

    if 750*time.Millisecond != locker.callTimeout {
        t.Fatalf("expected the options to reach the registered locker, got call timeout %v", locker.callTimeout)
    }
}

/* PIN of the door's contract, not a guard: the option-less registration builds the locker at its defaults, the bounded one */
func TestRegisterLockerService_KeepsTheDefaultCallTimeout(t *testing.T) {
    serviceContainer := container.NewContainer()
    registrar := &containerRegistrar{target: serviceContainer}

    RegisterLockerService(registrar, fakeClient{})

    locker := melodylock.LockerMustFromContainer(serviceContainer).(*Locker)
    if defaultLockerCallTimeout != locker.callTimeout {
        t.Fatalf("expected the option-less registration to keep the default call timeout, got %v", locker.callTimeout)
    }
}
