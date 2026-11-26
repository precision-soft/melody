package rueidis

import (
    "context"
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

func TestRegisterServices_UseTheConnectionLockerAndTokenStoreNames(t *testing.T) {
    registrar := &recordingRegistrar{}

    registerConnectionService(registrar, connectionOver(fakeClient{}), containercontract.RegisterOptions{})
    registerLockerService(registrar, LockerConfig{})
    registerTokenStoreService(registrar, TokenStoreConfig{})

    for _, name := range []string{ServiceConnection, melodylock.ServiceLocker, ServiceTokenStore} {
        if false == registrar.has(name) {
            t.Fatalf("expected %q to be registered, got %v", name, registrar.names)
        }
    }
}

func TestRegisterLockerAndTokenStoreService_ANoConnectionIsRefusedAtTheResolve(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    registerLockerService(registrar, LockerConfig{})
    registerTokenStoreService(registrar, TokenStoreConfig{})

    if _, lockerErr := container.FromResolver[any](t.Context(), serviceContainer, melodylock.ServiceLocker); nil == lockerErr {
        t.Fatal("expected a locker with no connection service refused")
    }

    if _, storeErr := container.FromResolver[any](t.Context(), serviceContainer, ServiceTokenStore); nil == storeErr {
        t.Fatal("expected a token store with no connection service refused")
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

/* through the owning Connection and the dependency edge, whichever run resolves a client-backed service closes the connection after it */
func TestRegisterConnectionService_TeardownClosesTheClientOnceTheClientWasResolved(t *testing.T) {
    client := &closeSpyClient{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    registerConnectionService(registrar, connectionOver(client), containercontract.RegisterOptions{})

    resolvedClient := ClientMustFromResolver(t.Context(), serviceContainer)
    if nil == resolvedClient {
        t.Fatalf("expected the client to resolve")
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("container close: %v", closeErr)
    }

    if false == client.closed {
        t.Fatalf("expected the teardown to close the client through the owning connection")
    }
}

/* the guarantee's declared boundary: the container closes only what was resolved at least once */
func TestRegisterConnectionService_TeardownLeavesAnUnresolvedConnectionOpen(t *testing.T) {
    client := &closeSpyClient{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    registerConnectionService(registrar, connectionOver(client), containercontract.RegisterOptions{})

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("container close: %v", closeErr)
    }

    if true == client.closed {
        t.Fatalf("expected an unresolved connection to stay the composition root's to close")
    }
}

func TestRegisterLockerService_HandsTheConfigToTheLocker(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    registerConnectionService(registrar, connectionOver(fakeClient{}), containercontract.RegisterOptions{})
    registerLockerService(registrar, LockerConfig{CallTimeout: bound.Of(750 * time.Millisecond)})

    locker, ok := melodylock.LockerMustFromResolver(t.Context(), serviceContainer).(*Locker)
    if false == ok {
        t.Fatalf("expected the registered locker to be this package's, got %T", melodylock.LockerMustFromResolver(t.Context(), serviceContainer))
    }

    if 750*time.Millisecond != locker.callTimeout {
        t.Fatalf("expected the config to reach the registered locker, got call timeout %v", locker.callTimeout)
    }
}

/* PIN of the door's contract, not a guard: an empty config builds the locker at its defaults, the bounded one */
func TestRegisterLockerService_KeepsTheDefaultCallTimeout(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    registerConnectionService(registrar, connectionOver(fakeClient{}), containercontract.RegisterOptions{})
    registerLockerService(registrar, LockerConfig{})

    locker := melodylock.LockerMustFromResolver(t.Context(), serviceContainer).(*Locker)
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

        NewModule(ModuleConfig{Connection: connectionOver(fakeClient{}), TokenStore: &testCase.config}).RegisterServices(&containerRegistrar{target: serviceContainer})

        store := TokenStoreMustFromResolver(t.Context(), serviceContainer).(*RedisTokenStore)
        if testCase.expected != store.clock {
            t.Fatalf("%s: expected the store's clock to be %v, got %v", name, testCase.expected, store.clock)
        }
    }
}

func TestRegisterTokenStoreService_HandsTheConfigToTheStore(t *testing.T) {
    config := TokenStoreConfig{Prefix: "app:token", ScanCount: bound.Of(64), CallTimeout: bound.Of(750 * time.Millisecond), MaximumClockSkew: bound.Of(time.Second)}

    for name, register := range map[string]func(serviceContainer containercontract.Container){
        "door": func(serviceContainer containercontract.Container) {
            registrar := &containerRegistrar{target: serviceContainer}
            registerConnectionService(registrar, connectionOver(fakeClient{}), containercontract.RegisterOptions{})
            registerTokenStoreService(registrar, config)
        },
        "module": func(serviceContainer containercontract.Container) {
            NewModule(ModuleConfig{Connection: connectionOver(fakeClient{}), TokenStore: &config}).RegisterServices(&containerRegistrar{target: serviceContainer})
        },
    } {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        register(serviceContainer)

        store := TokenStoreMustFromResolver(t.Context(), serviceContainer).(*RedisTokenStore)
        if "app:token" != store.prefix || 64 != store.scanCount || 750*time.Millisecond != store.callTimeout || time.Second != store.maximumClockSkew {
            t.Fatalf("%s: expected the config on the registered store, got prefix %q, scan %d, timeout %s, skew %s", name, store.prefix, store.scanCount, store.callTimeout, store.maximumClockSkew)
        }
    }
}

func (instance *containerRegistrar) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    return nil
}

func TestRegisterServices_TheRegisteredLockerAndTokenStoreDeriveTheirNamesFromTheConnection(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    shopConnection := func(resolver containercontract.Resolver) (*Connection, error) {
        return NewConnection(fakeClient{}, "shop"), nil
    }

    NewModule(ModuleConfig{Connection: shopConnection, Locker: &LockerConfig{}, TokenStore: &TokenStoreConfig{}}).RegisterServices(&containerRegistrar{target: serviceContainer})

    lock := mustCreateLock(melodylock.LockerMustFromResolver(t.Context(), serviceContainer).CreateLock("x", bound.Of(time.Minute)))
    if key := lock.(*redisLock).key; "shop:lock:x" != key {
        t.Fatalf("expected the registered locker to key shop:lock:x, got %q", key)
    }

    if store := TokenStoreMustFromResolver(t.Context(), serviceContainer).(*RedisTokenStore); "shop:token" != store.prefix {
        t.Fatalf("expected the registered token store under shop:token, got %q", store.prefix)
    }

    disabledContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{Connection: shopConnection, Locker: &LockerConfig{KeyPrefixDisabled: true}}).RegisterServices(&containerRegistrar{target: disabledContainer})

    bareLock := mustCreateLock(melodylock.LockerMustFromResolver(t.Context(), disabledContainer).CreateLock("x", bound.Of(time.Minute)))
    if key := bareLock.(*redisLock).key; "x" != key {
        t.Fatalf("expected the registered locker with its prefix disabled to key the bare name, got %q", key)
    }
}
