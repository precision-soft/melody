package amqp

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

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

type spyParameterRegistrar struct {
    names []string
}

func (instance *spyParameterRegistrar) RegisterParameter(name string, value any) {
    instance.names = append(instance.names, name)
}

func (instance *spyParameterRegistrar) RegisterSecretParameter(name string, value any) {
    instance.names = append(instance.names, name)
}

/* marking an existing parameter registers nothing, so the recorded names stay the set the module contributed */
func (instance *spyParameterRegistrar) MarkParameterSecret(name string) {
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

func TestModule_RegisterParametersRespectsFlag(t *testing.T) {
    registrar := &spyParameterRegistrar{}
    NewModule(ModuleConfig{}).RegisterParameters(registrar)
    if 0 != len(registrar.names) {
        t.Fatalf("expected no parameters without the flag, got %v", registrar.names)
    }

    registrar = &spyParameterRegistrar{}
    NewModule(ModuleConfig{WithDefaultParameters: true}).RegisterParameters(registrar)
    if false == containsName(registrar.names, ParameterDsn) || false == containsName(registrar.names, ParameterPrefetch) {
        t.Fatalf("expected default parameters, got %v", registrar.names)
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
        Connection: &amqp091.Connection{},
        Transports: map[string]*Transport{"orders": {}},
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
        Connection: &amqp091.Connection{},
        Transports: map[string]*Transport{"discarded": nil},
    }).RegisterServices(registrar)
}

func TestModule_ACloseTimeoutWithNoConnectionIsRefusedNamingIt(t *testing.T) {
    expectPanicNamingField(t, "ModuleConfig.CloseTimeout", func() {
        NewModule(ModuleConfig{CloseTimeout: bound.Of(time.Second)}).RegisterServices(&spyServiceRegistrar{})
    })
}

func TestModule_AnAbsentCloseTimeoutWithNoConnectionRegistersNothing(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{CloseTimeout: bound.Duration{}}).RegisterServices(registrar)

    if 0 != len(registrar.names) {
        t.Fatalf("expected no services, got %v", registrar.names)
    }
}

func TestModule_TransportsWithNoConnectionAreRegistered(t *testing.T) {
    registrar := &spyServiceRegistrar{}
    NewModule(ModuleConfig{Transports: map[string]*Transport{"orders": {}}}).RegisterServices(registrar)

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

            NewModule(ModuleConfig{Transports: map[string]*Transport{"gamma": nil, "alpha": nil, "beta": nil, "delta": nil}}).RegisterServices(&spyServiceRegistrar{})
        }()
    }
}
