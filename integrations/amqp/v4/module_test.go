package amqp

import (
    "context"
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
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
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

        if "discarded" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["transport"] {
            t.Fatalf("expected the refusal to name the transport, got %v", exception.LogContext(recoveredErr, exception.LogContextConfig{}))
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

/* the fake broker never answers the close, so the container's teardown of the registered connection ends at the close timeout the owner carries from its provider, the module having none of its own */
func TestModule_HasNoCloseTimeoutOfItsOwn(t *testing.T) {
    address := listenFakeBroker(t)
    provider := NewProvider(ProviderConfig{CloseTimeout: bound.Of(200 * time.Millisecond)})

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
                if "alpha" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["transport"] {
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
