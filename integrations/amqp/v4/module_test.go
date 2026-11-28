package amqp

import (
    "context"
    "strings"
    "testing"
    "time"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

type transportProviders = map[string]func(resolver containercontract.Resolver) (*Transport, error)

func staticConnectionFactory(resolver containercontract.Resolver) (*Connection, error) {
    return NewConnection(&amqp091.Connection{}, ConnectionConfig{}), nil
}

func emptyTransportProvider(resolver containercontract.Resolver) (*Transport, error) {
    return &Transport{}, nil
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

    if "amqp" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "amqp")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_ImplementsNoParameterHook(t *testing.T) {
    if _, isParameterModule := any(NewModule(ModuleConfig{})).(applicationcontract.ParameterModule); true == isParameterModule {
        t.Fatalf("expected the module to register no parameter")
    }
}

func TestModule_RegisterServices(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{}).RegisterServices(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no services without a connection or transports, got %v", registrar.names)
    }

    registrar = &spyServiceRegistrar{}
    NewModule(ModuleConfig{
        Connection: staticConnectionFactory,
        Transports: transportProviders{"orders": emptyTransportProvider},
    }).RegisterServices(registrar)

    if false == containsName(registrar.names, ServiceConnection) {
        t.Fatalf("expected the connection service, got %v", registrar.names)
    }
    if false == containsName(registrar.names, "orders") {
        t.Fatalf("expected the orders transport service, got %v", registrar.names)
    }
}

func TestModule_ANilTransportIsRefusedNamingItBeforeAnythingIsRegistered(t *testing.T) {
    registrar := &spyServiceRegistrar{}

    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatal("expected a nil transport to be refused at boot")
        }

        expectErrorNamingField(t, recoveredErr, "ModuleConfig.Transports")

        if "discarded" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["transport"] {
            t.Fatalf("expected the refusal to name the transport, got %v", exception.LogContext(recoveredErr, exception.LogContextOptions{}))
        }

        if 0 != len(registrar.names) {
            t.Fatalf("expected nothing registered before the refusal, got %v", registrar.names)
        }
    }()

    NewModule(ModuleConfig{
        Connection: staticConnectionFactory,
        Transports: transportProviders{"discarded": nil},
    }).RegisterServices(registrar)
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
        {"a failure mode with a connection not built at boot", ModuleConfig{Connection: staticConnectionFactory, ConnectionBootFailure: container.BootFailureFailsBoot}, "ModuleConfig.ConnectionBootFailure", "ModuleConfig.ConnectionBuiltAtBoot"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            registrar := &spyServiceRegistrar{}

            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatal("expected the boot mode refused")
                }

                context := exception.LogContext(recoveredErr, exception.LogContextOptions{})
                if testCase.field != context["field"] || testCase.requires != context["requires"] || false == strings.Contains(recoveredErr.Error(), "has no connection built at boot") {
                    t.Fatalf("expected the refusal to name %s and %s, got %v: %v", testCase.field, testCase.requires, context, recoveredErr)
                }

                if 0 != len(registrar.names) {
                    t.Fatalf("expected nothing registered before the refusal, got %v", registrar.names)
                }
            }()

            NewModule(testCase.config).RegisterServices(registrar)
        })
    }
}

func TestModule_ABootModeIsRefusedAfterANilTransport(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatal("expected the module refused")
        }

        expectErrorNamingField(t, recoveredErr, "ModuleConfig.Transports")
    }()

    NewModule(ModuleConfig{ConnectionBuiltAtBoot: true, Transports: transportProviders{"orders": nil}}).RegisterServices(&spyServiceRegistrar{})
}

func TestModule_TheConnectionIsBuiltAtBootWithItsFailureMode(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{
        Connection:            staticConnectionFactory,
        ConnectionBuiltAtBoot: true,
        ConnectionBootFailure: container.BootFailureFailsBoot,
        Transports:            transportProviders{"orders": emptyTransportProvider},
    }).RegisterServices(registrar)

    options, registered := registrar.options[ServiceConnection]
    if false == registered || false == options.BuiltAtBoot || container.BootFailureFailsBoot != options.BootFailure || false == options.TeardownReflectionDisabled {
        t.Fatalf("expected the connection registered built at boot with its mode and not walked, got %+v (registered %v)", options, registered)
    }

    if true == registrar.options["orders"].BuiltAtBoot {
        t.Fatalf("expected a transport built at its first resolution, got %+v", registrar.options["orders"])
    }

    warns := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Connection: staticConnectionFactory, ConnectionBuiltAtBoot: true}).RegisterServices(warns)
    if false == warns.options[ServiceConnection].BuiltAtBoot || container.BootFailureWarns != warns.options[ServiceConnection].BootFailure {
        t.Fatalf("expected the warning mode when left out, got %+v", warns.options[ServiceConnection])
    }

    plain := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Connection: staticConnectionFactory}).RegisterServices(plain)
    if true == plain.options[ServiceConnection].BuiltAtBoot || false == plain.options[ServiceConnection].TeardownReflectionDisabled {
        t.Fatalf("expected a connection not asked at boot built at its first resolution and not walked, got %+v", plain.options[ServiceConnection])
    }
}

/* the application's Boot builds what the container answers here */
func TestModule_AConnectionBuiltAtBootIsBuiltByTheContainersBootBuild(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{
        Connection:            staticConnectionFactory,
        ConnectionBuiltAtBoot: true,
        ConnectionBootFailure: container.BootFailureFailsBoot,
        Transports:            transportProviders{"orders": emptyTransportProvider},
    }).RegisterServices(&containerRegistrar{serviceContainer: serviceContainer})

    registrations := serviceContainer.(interface {
        BuiltAtBootRegistrations() []containercontract.BuiltAtBootRegistration
    }).BuiltAtBootRegistrations()

    if 1 != len(registrations) || ServiceConnection != registrations[0].ServiceName || container.BootFailureFailsBoot != registrations[0].BootFailure {
        t.Fatalf("expected the connection alone built at boot with its mode, got %+v", registrations)
    }
}

/* the fake broker never answers the close, so the container's teardown of the registered connection ends at the close timeout the owner carries from its provider, the module having none of its own */
func TestModule_HasNoCloseTimeoutOfItsOwn(t *testing.T) {
    address := listenFakeBroker(t)
    provider := NewProvider("melody", ProviderConfig{CloseTimeout: bound.Of(200 * time.Millisecond)})

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{
        Connection: func(resolver containercontract.Resolver) (*Connection, error) {
            return provider.Open(resolver.Runtime(), "amqp://guest:guest@"+address+"/?heartbeat=0")
        },
    }).RegisterServices(&containerRegistrar{serviceContainer: serviceContainer})

    ConnectionMustFromResolver(t.Context(), serviceContainer)

    started := time.Now()
    _ = serviceContainer.(containercontract.ContextCloser).CloseWithContext(context.Background())

    if elapsed := time.Since(started); 5*time.Second <= elapsed {
        t.Fatalf("expected the teardown bounded by the provider's 200ms close timeout, it took %s", elapsed)
    }
}

/* a transport provider resolves the shared connection, which a transport built at wiring could not */
func TestModule_ATransportProviderBuildsOverTheResolvedConnection(t *testing.T) {
    raw := &amqp091.Connection{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{
        Connection: func(resolver containercontract.Resolver) (*Connection, error) {
            return NewConnection(raw, ConnectionConfig{}), nil
        },
        Transports: transportProviders{"melody.amqp.v4.orders": func(resolver containercontract.Resolver) (*Transport, error) {
            return NewTransport("melody.amqp.v4.orders", NewMessageRegistry(), FromConnection(ConnectionMustFromResolver(resolver.Runtime().Context(), resolver)), TransportConfig{}), nil
        }},
    }).RegisterServices(&containerRegistrar{serviceContainer: serviceContainer})

    transport, isTransport := TransportMustFromResolver(t.Context(), serviceContainer, "melody.amqp.v4.orders").(*Transport)
    if false == isTransport || raw != transport.connection || true == transport.ownsConnection {
        t.Fatalf("expected the transport to borrow the registered connection, got %+v", transport)
    }
}

func TestModule_ATransportProviderAnsweringNoTransportIsRefusedAtResolution(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    NewModule(ModuleConfig{
        Transports: transportProviders{"orders": func(resolver containercontract.Resolver) (*Transport, error) { return nil, nil }},
    }).RegisterServices(&containerRegistrar{serviceContainer: serviceContainer})

    _, resolveErr := container.FromResolver[messagebuscontract.Transport](t.Context(), serviceContainer, "orders")
    if nil == resolveErr || false == errorChainContains(resolveErr, "the amqp transport provider answered no transport and no error") {
        t.Fatalf("expected a provider answering no transport refused naming itself, got %v", resolveErr)
    }
}

func TestModule_TransportsWithNoConnectionAreRegistered(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Transports: transportProviders{"orders": emptyTransportProvider}}).RegisterServices(registrar)

    if 1 != len(registrar.names) || "orders" != registrar.names[0] {
        t.Fatalf("expected only the orders transport service, got %v", registrar.names)
    }
}

func TestModule_SeveralNilTransportsAreRefusedNamingTheFirstInNameOrder(t *testing.T) {
    for range 20 {
        func() {
            defer func() {
                recoveredErr, _ := recover().(error)
                if "alpha" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["transport"] {
                    t.Fatalf("expected the refusal to name alpha, got %v", recoveredErr)
                }
            }()

            NewModule(ModuleConfig{Transports: transportProviders{"gamma": nil, "alpha": nil, "beta": nil, "delta": nil}}).RegisterServices(&spyServiceRegistrar{})
        }()
    }
}

func (instance *spyServiceRegistrar) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    return nil
}
