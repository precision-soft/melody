package config

import (
    "context"
    "strings"
    "testing"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

func newNamespacedConfiguration(t *testing.T, values map[string]string, namespace string) (*Configuration, error) {
    t.Helper()

    environment, environmentErr := NewEnvironment(context.Background(), &testEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    return NewConfiguration(environment, "/tmp/melody", ConfigurationConfig{Namespace: namespace})
}

func TestNamespace_TheKeyWinsOverTheFieldAndTheDefaultIsMelody(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        values   map[string]string
        field    string
        expected string
    }{
        {name: "the key wins over the field", values: map[string]string{NamespaceKey: "shop"}, field: "store", expected: "shop"},
        {name: "the field when the key is unset", values: map[string]string{}, field: "store", expected: "store"},
        {name: "melody when neither names one", values: map[string]string{}, field: "", expected: DefaultNamespace},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            configuration, configurationErr := newNamespacedConfiguration(t, testCase.values, testCase.field)
            if nil != configurationErr {
                t.Fatalf("unexpected configuration error: %v", configurationErr)
            }

            if testCase.expected != configuration.Kernel().Namespace() {
                t.Fatalf("expected the namespace %q, got %q", testCase.expected, configuration.Kernel().Namespace())
            }
        })
    }

    if "melody" != DefaultNamespace {
        t.Fatalf("expected the default namespace to keep the third major's spelling, got %q", DefaultNamespace)
    }
}

func TestNamespace_AMalformedValueRefusesTheBootNamingItsSource(t *testing.T) {
    malformed := []string{"a:b", "a.b", "A", "-a", "a-", "_a", "a b", "a*", strings.Repeat("a", 33)}

    for _, value := range malformed {
        _, keyErr := newNamespacedConfiguration(t, map[string]string{NamespaceKey: value}, "")
        if nil == keyErr || NamespaceKey != exception.FromError(keyErr).Context()["key"] || value != exception.FromError(keyErr).Context()["value"] {
            t.Fatalf("expected the key value %q refused naming the key, got %v", value, keyErr)
        }

        _, fieldErr := newNamespacedConfiguration(t, map[string]string{}, value)
        if nil == fieldErr || "ApplicationConfig.Namespace" != exception.FromError(fieldErr).Context()["field"] {
            t.Fatalf("expected the field value %q refused naming the field, got %v", value, fieldErr)
        }
    }

    _, underKeyErr := newNamespacedConfiguration(t, map[string]string{NamespaceKey: "shop"}, "a:b")
    if nil == underKeyErr || "ApplicationConfig.Namespace" != exception.FromError(underKeyErr).Context()["field"] {
        t.Fatalf("expected a malformed field refused although the key is set, got %v", underKeyErr)
    }
}

func TestValidateNamespace_TheRule(t *testing.T) {
    for _, value := range []string{"melody", "a", "0", "shop-eu_1", "melody-example-v4", strings.Repeat("z", 32)} {
        if validateErr := ValidateNamespace(value); nil != validateErr {
            t.Fatalf("expected %q admitted, got %v", value, validateErr)
        }
    }

    for _, value := range []string{"", "a:b", "a.b", "Shop", "-a", "a_", "a/b", "a\tb", strings.Repeat("z", 33)} {
        validateErr := ValidateNamespace(value)
        if nil == validateErr || value != exception.FromError(validateErr).Context()["value"] {
            t.Fatalf("expected %q refused naming the value, got %v", value, validateErr)
        }
    }
}

type kernelAnsweringConfiguration struct {
    configcontract.Configuration
    kernel configcontract.KernelConfiguration
}

func (instance kernelAnsweringConfiguration) Kernel() configcontract.KernelConfiguration {
    return instance.kernel
}

type namespaceAnsweringKernel struct {
    configcontract.KernelConfiguration
    namespace string
}

func (instance *namespaceAnsweringKernel) Namespace() string {
    return instance.namespace
}

func runtimeOverConfiguration(t *testing.T, configuration configcontract.Configuration) containercontract.Runtime {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    t.Cleanup(func() { _ = serviceContainer.CloseWithContext(context.Background()) })

    if nil != configuration {
        serviceContainer.MustRegister(
            ServiceConfig,
            func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
                return configuration, nil
            },
            container.RegisterOptions{},
        )
    }

    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}

/* typedNilConfigurationScope stands for a foreign scope, the only resolver that can answer a typed-nil configuration: the framework's container refuses a nil value. */
type typedNilConfigurationScope struct {
    containercontract.Scope
}

func (instance typedNilConfigurationScope) Get(ctx context.Context, serviceName string) (any, error) {
    if ServiceConfig == serviceName {
        var configuration *Configuration

        return configuration, nil
    }

    return instance.Scope.Get(ctx, serviceName)
}

type foreignScopeRuntime struct {
    containercontract.Runtime
    scope containercontract.Scope
}

func (instance foreignScopeRuntime) Scope() containercontract.Scope {
    return instance.scope
}

func TestNamespaceFromRuntime_AConfigurationResolvedAsATypedNilIsTheDefault(t *testing.T) {
    realRuntime := runtimeOverConfiguration(t, nil)
    foreignRuntime := foreignScopeRuntime{Runtime: realRuntime, scope: typedNilConfigurationScope{Scope: realRuntime.Scope()}}

    if namespace := NamespaceFromRuntime(foreignRuntime); DefaultNamespace != namespace {
        t.Fatalf("expected the default namespace for a typed-nil configuration, got %q", namespace)
    }
}

func TestNamespaceFromRuntime_AnswersTheConfigurationsNamespaceElseTheDefault(t *testing.T) {
    namespaced, namespacedErr := newNamespacedConfiguration(t, map[string]string{}, "shop")
    if nil != namespacedErr {
        t.Fatalf("unexpected configuration error: %v", namespacedErr)
    }

    var typedNilKernel *namespaceAnsweringKernel

    for _, testCase := range []struct {
        name          string
        configuration configcontract.Configuration
        expected      string
    }{
        {name: "the configuration's namespace", configuration: namespaced, expected: "shop"},
        {name: "no configuration", configuration: nil, expected: DefaultNamespace},
        {name: "a kernel configuration that is a typed nil", configuration: kernelAnsweringConfiguration{kernel: typedNilKernel}, expected: DefaultNamespace},
        {name: "a kernel configuration that is nil", configuration: kernelAnsweringConfiguration{}, expected: DefaultNamespace},
        {name: "a kernel configuration naming none", configuration: kernelAnsweringConfiguration{kernel: &namespaceAnsweringKernel{}}, expected: DefaultNamespace},
        {name: "a kernel configuration naming one", configuration: kernelAnsweringConfiguration{kernel: &namespaceAnsweringKernel{namespace: "billing"}}, expected: "billing"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            if namespace := NamespaceFromRuntime(runtimeOverConfiguration(t, testCase.configuration)); testCase.expected != namespace {
                t.Fatalf("expected the namespace %q, got %q", testCase.expected, namespace)
            }
        })
    }
}
