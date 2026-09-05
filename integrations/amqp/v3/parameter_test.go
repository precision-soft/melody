package amqp

import (
    "testing"
)

type parameterSpyRegistrar struct {
    values map[string]any
    order  []string
}

func newParameterSpyRegistrar() *parameterSpyRegistrar {
    return &parameterSpyRegistrar{values: make(map[string]any)}
}

func (instance *parameterSpyRegistrar) RegisterParameter(name string, value any) {
    instance.values[name] = value
    instance.order = append(instance.order, name)
}

/* the parameter NAMES are the wire between the module's defaults and whatever an application writes in its configuration: a name that drifts here leaves the application's own value unread while everything still compiles */
func TestAmqpParameterNames_AreTheConfigurationNames(t *testing.T) {
    for _, probe := range []struct {
        actual   string
        expected string
    }{
        {actual: ParameterDsn, expected: "melody.amqp.dsn"},
        {actual: ParameterExchange, expected: "melody.amqp.exchange"},
        {actual: ParameterPrefetch, expected: "melody.amqp.prefetch"},
    } {
        if probe.expected != probe.actual {
            t.Fatalf("expected the configuration name %q, got %q", probe.expected, probe.actual)
        }
    }
}

func TestRegisterDefaultParameters_RegistersTheDefaultsUnderTheirNames(t *testing.T) {
    registrar := newParameterSpyRegistrar()

    RegisterDefaultParameters(registrar)

    if "amqp://guest:guest@localhost:5672/" != registrar.values[ParameterDsn] {
        t.Fatalf("expected the default dsn, got %v", registrar.values[ParameterDsn])
    }

    if 10 != registrar.values[ParameterPrefetch] {
        t.Fatalf("expected the default prefetch, got %v", registrar.values[ParameterPrefetch])
    }
}

/* the exchange deliberately gets NO default: it names the application's own topology, and a default would silently bind every application that forgot to declare one onto the same exchange */
func TestRegisterDefaultParameters_LeavesTheExchangeToTheApplication(t *testing.T) {
    registrar := newParameterSpyRegistrar()

    RegisterDefaultParameters(registrar)

    if _, registered := registrar.values[ParameterExchange]; true == registered {
        t.Fatalf("expected the exchange to be left undeclared, got %v", registrar.values[ParameterExchange])
    }

    if 2 != len(registrar.order) {
        t.Fatalf("expected exactly the two defaults to be registered, got %v", registrar.order)
    }
}

var _ ParameterRegistrar = (*parameterSpyRegistrar)(nil)
