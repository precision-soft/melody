package rueidis

import (
    "testing"
    "time"

    "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
)

type recordingRegistrar struct {
    names []string
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
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

    RegisterLockerService(registrar, fakeClient{}, LockerConfig{})

    if false == registrar.has(melodylock.ServiceLocker) {
        t.Fatalf("expected %q to be registered, got %v", melodylock.ServiceLocker, registrar.names)
    }
}

func TestRegisterLockerAndTokenStoreService_RefuseANilClientAtRegistration(t *testing.T) {
    for door, register := range map[string]func(){
        "RegisterLockerService":     func() { RegisterLockerService(&recordingRegistrar{}, nil, LockerConfig{}) },
        "RegisterTokenStoreService": func() { RegisterTokenStoreService(&recordingRegistrar{}, nil, TokenStoreConfig{}) },
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("expected %s to refuse a nil client at registration", door)
                }
            }()

            register()
        }()
    }
}

func TestRegisterTokenStoreServiceUsesTokenStoreName(t *testing.T) {
    registrar := &recordingRegistrar{}

    RegisterTokenStoreService(registrar, fakeClient{}, TokenStoreConfig{})

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

func (instance *containerRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.target.MustRegister(serviceName, provider, options)
}

func (instance *containerRegistrar) Register(serviceName string, provider any, options containercontract.RegisterOptions) error {
    return instance.target.Register(serviceName, provider, options)
}

func (instance *containerRegistrar) MustRegister(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.target.MustRegister(serviceName, provider, options)
}

/* the raw client's Close returns nothing, so registered alone it could never join the container's teardown and the connection lived exactly as long as the process; through the owning Connection and the dependency edge, whichever run resolves a client-backed service closes the connection after it */
func TestRegisterConnectionService_TeardownClosesTheClientOnceTheClientWasResolved(t *testing.T) {
    client := &closeSpyClient{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
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
    serviceContainer := container.NewContainer(container.ContainerConfig{})
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

func TestRegisterLockerService_HandsTheConfigToTheLocker(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    RegisterLockerService(registrar, fakeClient{}, LockerConfig{CallTimeout: bound.Of(750 * time.Millisecond)})

    locker, ok := melodylock.LockerMustFromContainer(serviceContainer).(*Locker)
    if false == ok {
        t.Fatalf("expected the registered locker to be this package's, got %T", melodylock.LockerMustFromContainer(serviceContainer))
    }

    if 750*time.Millisecond != locker.callTimeout {
        t.Fatalf("expected the config to reach the registered locker, got call timeout %v", locker.callTimeout)
    }
}

/* PIN of the door's contract, not a guard: an empty config builds the locker at its defaults, the bounded one */
func TestRegisterLockerService_KeepsTheDefaultCallTimeout(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    RegisterLockerService(registrar, fakeClient{}, LockerConfig{})

    locker := melodylock.LockerMustFromContainer(serviceContainer).(*Locker)
    if defaultLockerCallTimeout != locker.callTimeout {
        t.Fatalf("expected the option-less registration to keep the default call timeout, got %v", locker.callTimeout)
    }
}

func TestRegisterTokenStoreService_TheContainerClockFillsOnlyAnAbsentConfigClock(t *testing.T) {
    containerClock := melodyclock.NewFrozenClock(time.Unix(1000, 0))
    configClock := melodyclock.NewFrozenClock(time.Unix(2000, 0))

    for name, testCase := range map[string]struct {
        config   TokenStoreConfig
        expected clockcontract.Clock
    }{
        "absent": {config: TokenStoreConfig{}, expected: containerClock},
        "named":  {config: TokenStoreConfig{Clock: configClock}, expected: configClock},
    } {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        serviceContainer.MustRegister(
            melodyclock.ServiceClock,
            func(containercontract.Resolver) (clockcontract.Clock, error) {
                return containerClock, nil
            },
            container.RegisterOptions{},
        )

        RegisterTokenStoreService(&containerRegistrar{target: serviceContainer}, fakeClient{}, testCase.config)

        store := TokenStoreMustFromContainer(serviceContainer).(*RedisTokenStore)
        if testCase.expected != store.clock {
            t.Fatalf("%s: expected the store's clock to be %v, got %v", name, testCase.expected, store.clock)
        }
    }
}

func TestRegisterTokenStoreService_HandsTheConfigToTheStore(t *testing.T) {
    config := TokenStoreConfig{Prefix: "app:token", ScanCount: bound.Of(64), CallTimeout: bound.Of(750 * time.Millisecond), MaximumClockSkew: bound.Of(time.Second)}

    for name, register := range map[string]func(serviceContainer containercontract.Container){
        "door": func(serviceContainer containercontract.Container) {
            RegisterTokenStoreService(&containerRegistrar{target: serviceContainer}, fakeClient{}, config)
        },
        "module": func(serviceContainer containercontract.Container) {
            NewModule(ModuleConfig{Client: fakeClient{}, AsTokenStore: true, TokenStore: config}).RegisterServices(&containerRegistrar{target: serviceContainer})
        },
    } {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        register(serviceContainer)

        store := TokenStoreMustFromContainer(serviceContainer).(*RedisTokenStore)
        if "app:token" != store.prefix || 64 != store.scanCount || 750*time.Millisecond != store.callTimeout || time.Second != store.maximumClockSkew {
            t.Fatalf("%s: expected the config on the registered store, got prefix %q, scan %d, timeout %s, skew %s", name, store.prefix, store.scanCount, store.callTimeout, store.maximumClockSkew)
        }
    }
}
