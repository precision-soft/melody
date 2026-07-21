package amqp

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

type spyServiceRegistrar struct {
    names []string
}

func (instance *spyServiceRegistrar) RegisterService(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.names = append(instance.names, serviceName)
}

func (instance *spyServiceRegistrar) Register(serviceName string, provider any, options ...containercontract.RegisterOption) error {
    instance.RegisterService(serviceName, provider, options...)

    return nil
}

func (instance *spyServiceRegistrar) MustRegister(serviceName string, provider any, options ...containercontract.RegisterOption) {
    instance.RegisterService(serviceName, provider, options...)
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
        Transports: map[string]*Transport{"orders": {}, "skipped": nil},
    }).RegisterServices(registrar)

    if false == containsName(registrar.names, ServiceConnection) {
        t.Fatalf("expected the connection service, got %v", registrar.names)
    }
    if false == containsName(registrar.names, "orders") {
        t.Fatalf("expected the orders transport service, got %v", registrar.names)
    }
    if true == containsName(registrar.names, "skipped") {
        t.Fatalf("expected nil transports to be skipped, got %v", registrar.names)
    }
}
