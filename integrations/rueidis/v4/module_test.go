package rueidis

import (
    "strings"
    "testing"
    "time"

    "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
)

type fakeClient struct {
    rueidis.Client
}

type spyServiceRegistrar struct {
    names   []string
    options map[string]containercontract.RegisterOptions
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)

    if nil == instance.options {
        instance.options = map[string]containercontract.RegisterOptions{}
    }

    instance.options[serviceName] = options
}

func (instance *spyServiceRegistrar) Register(serviceName string, provider any, options containercontract.RegisterOptions) error {
    instance.RegisterService(serviceName, provider, options)

    return nil
}

func (instance *spyServiceRegistrar) MustRegister(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.RegisterService(serviceName, provider, options)
}

func containsName(names []string, want string) bool {
    for _, name := range names {
        if want == name {
            return true
        }
    }

    return false
}

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "rueidis" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "rueidis")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_AnEmptyConfigRegistersNothing(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no services from an empty module, got %v", registrar.names)
    }
}

func TestModule_ARoleWithoutAConnectionIsRefused(t *testing.T) {
    expectPanicNamingField(t, "ModuleConfig.Locker", func() {
        NewModule(ModuleConfig{Locker: &LockerConfig{}}).RegisterServices(&spyServiceRegistrar{})
    })

    expectPanicNamingField(t, "ModuleConfig.TokenStore", func() {
        NewModule(ModuleConfig{TokenStore: &TokenStoreConfig{}}).RegisterServices(&spyServiceRegistrar{})
    })
}

func TestModule_ABootModeWithoutAConnectionBuiltAtBootIsRefused(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        config   ModuleConfig
        field    string
        requires string
    }{
        {"built at boot with no connection", ModuleConfig{ConnectionBuiltAtBoot: true}, "ModuleConfig.ConnectionBuiltAtBoot", "ModuleConfig.Connection"},
        {"a failure mode with no connection", ModuleConfig{ConnectionBootFailure: container.BootFailureFailsBoot}, "ModuleConfig.ConnectionBootFailure", "ModuleConfig.Connection"},
        {"a failure mode with a connection not built at boot", ModuleConfig{Connection: connectionOver(fakeClient{}), ConnectionBootFailure: container.BootFailureFailsBoot}, "ModuleConfig.ConnectionBootFailure", "ModuleConfig.ConnectionBuiltAtBoot"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatal("expected the boot mode refused")
                }

                context := exceptionContextOf(recoveredErr)
                if testCase.field != context["field"] || testCase.requires != context["requires"] || false == strings.Contains(recoveredErr.Error(), "has no connection built at boot") {
                    t.Fatalf("expected the refusal to name %s and %s, got %v: %v", testCase.field, testCase.requires, context, recoveredErr)
                }
            }()

            NewModule(testCase.config).RegisterServices(&spyServiceRegistrar{})
        })
    }
}

func TestModule_TheConnectionIsBuiltAtBootUnderTheBootRuntime(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    NewModule(ModuleConfig{
        Connection:            connectionOver(fakeClient{}),
        ConnectionBuiltAtBoot: true,
        ConnectionBootFailure: container.BootFailureFailsBoot,
    }).RegisterServices(registrar)

    options, registered := registrar.options[ServiceConnection]
    if false == registered || false == options.BuiltAtBoot || container.BootFailureFailsBoot != options.BootFailure {
        t.Fatalf("expected the connection registered built at boot with its mode, got %+v (registered %v)", options, registered)
    }

    plain := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Connection: connectionOver(fakeClient{})}).RegisterServices(plain)
    if true == plain.options[ServiceConnection].BuiltAtBoot {
        t.Fatalf("expected a connection not asked at boot to be built at its first resolve")
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{
        Connection:            connectionOver(fakeClient{}),
        ConnectionBuiltAtBoot: true,
        Locker:                &LockerConfig{},
        TokenStore:            &TokenStoreConfig{},
    }).RegisterServices(&containerRegistrar{target: serviceContainer})

    connection := container.MustFromResolver[*Connection](t.Context(), serviceContainer, ServiceConnection)
    locker := melodylock.LockerMustFromResolver(t.Context(), serviceContainer).(*Locker)
    store := TokenStoreMustFromResolver(t.Context(), serviceContainer).(*RedisTokenStore)

    if connection.Client() != locker.client || connection.Client() != store.client {
        t.Fatalf("expected the roles to run on the one owner's client")
    }
}

func TestModule_ANilRoleRegistersNothingAndAPointerRegistersIt(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Connection: connectionOver(fakeClient{})}).RegisterServices(registrar)

    if false == containsName(registrar.names, ServiceConnection) {
        t.Fatalf("expected the connection service, got %v", registrar.names)
    }
    if true == containsName(registrar.names, melodylock.ServiceLocker) || true == containsName(registrar.names, ServiceTokenStore) {
        t.Fatalf("expected no role without its configuration, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{Connection: connectionOver(fakeClient{}), Locker: &LockerConfig{}, TokenStore: &TokenStoreConfig{}}).RegisterServices(registrar)

    if false == containsName(registrar.names, melodylock.ServiceLocker) || false == containsName(registrar.names, ServiceTokenStore) {
        t.Fatalf("expected both roles behind their zero configuration, got %v", registrar.names)
    }
}

func TestModule_TheRoleConfigIsCopiedAtConstruction(t *testing.T) {
    lockerConfig := &LockerConfig{CallTimeout: bound.Of(750 * time.Millisecond)}
    tokenStoreConfig := &TokenStoreConfig{Prefix: "app:token"}

    module := NewModule(ModuleConfig{Connection: connectionOver(fakeClient{}), Locker: lockerConfig, TokenStore: tokenStoreConfig})

    lockerConfig.CallTimeout = bound.UnlimitedDuration()
    tokenStoreConfig.Prefix = "edited:"

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    module.RegisterServices(&containerRegistrar{target: serviceContainer})

    locker := melodylock.LockerMustFromResolver(t.Context(), serviceContainer).(*Locker)
    if 750*time.Millisecond != locker.callTimeout {
        t.Fatalf("expected the locker config of the construction, got %v", locker.callTimeout)
    }

    store := TokenStoreMustFromResolver(t.Context(), serviceContainer).(*RedisTokenStore)
    if "app:token" != store.prefix {
        t.Fatalf("expected the token store config of the construction, got %q", store.prefix)
    }
}

func TestModule_ARefusedLockerOrTokenStoreConfigFailsRegistration(t *testing.T) {
    expectPanicNamingField(t, "LockerConfig.CallTimeout", func() {
        NewModule(ModuleConfig{Connection: connectionOver(fakeClient{}), Locker: &LockerConfig{CallTimeout: bound.UnlimitedDuration()}}).RegisterServices(&spyServiceRegistrar{})
    })

    expectPanicNamingField(t, "TokenStoreConfig.ScanCount", func() {
        NewModule(ModuleConfig{Connection: connectionOver(fakeClient{}), TokenStore: &TokenStoreConfig{ScanCount: bound.Of(0)}}).RegisterServices(&spyServiceRegistrar{})
    })
}

func (instance *spyServiceRegistrar) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    return nil
}
