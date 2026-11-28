package rueidis

import (
    "context"
    "crypto/tls"
    "fmt"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type plaintextAddressEnvironmentSource struct {
    values map[string]string
}

func (instance *plaintextAddressEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

func newPlaintextAddressRuntime(t *testing.T, ctx context.Context, values map[string]string) (containercontract.Runtime, *capturingLogger) {
    t.Helper()

    logger := &capturingLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[loggingcontract.Logger](serviceContainer, logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{})

    if nil != values {
        environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &plaintextAddressEnvironmentSource{values: values})
        if nil != environmentErr {
            t.Fatalf("environment: %v", environmentErr)
        }

        configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir(), melodyconfig.ConfigurationConfig{})
        if nil != configurationErr {
            t.Fatalf("configuration: %v", configurationErr)
        }

        container.MustRegister[configcontract.Configuration](serviceContainer, melodyconfig.ServiceConfig, func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        }, container.RegisterOptions{})
    }

    return runtime.NewRun(ctx, serviceContainer.NewScope()), logger
}

func plaintextAddressWarnings(logger *capturingLogger) []capturedRecord {
    warnings := []capturedRecord{}
    for _, record := range logger.records {
        if PlaintextAddressBootWarningName == record.context[bootWarningContextKey] {
            warnings = append(warnings, record)
        }
    }

    return warnings
}

func TestPlaintextAddressesOf_OnlyAnAddressOffThisMachineIsNamed(t *testing.T) {
    expected := []string{"redis.example.com:6379", "redis:6379", "10.0.0.5:6379"}
    actual := plaintextAddressesOf([]string{"redis.example.com:6379", "localhost:6379", "redis:6379", "127.0.0.1:6379", "[::1]:6379", "10.0.0.5:6379", "LOCALHOST"})

    if fmt.Sprint(expected) != fmt.Sprint(actual) {
        t.Fatalf("expected %v, got %v", expected, actual)
    }
}

func TestPlaintextAddressJudge_NamesEachAddressOnce(t *testing.T) {
    runtimeInstance, logger := newPlaintextAddressRuntime(t, t.Context(), nil)
    judge := &plaintextAddressJudge{}
    for range 2 {
        judge.judge(runtimeInstance, []string{"redis.example.com:6379", "localhost:6379", "replica.example.com:6379"})
    }

    warnings := plaintextAddressWarnings(logger)
    if 2 != len(warnings) || plaintextAddressWarningMessage != warnings[0].message || "redis.example.com:6379" != warnings[0].context["address"] || "replica.example.com:6379" != warnings[1].context["address"] {
        t.Fatalf("expected one warning per address off this machine, got %+v", warnings)
    }
}

func TestPlaintextAddressJudge_HonoursTheBootWarningIgnoreKey(t *testing.T) {
    for name, testCase := range map[string]struct {
        values   map[string]string
        expected int
    }{
        "no key":               {values: map[string]string{}, expected: 2},
        "the code ignored":     {values: map[string]string{melodyconfig.BootWarningsIgnoredKey: PlaintextAddressBootWarningName}, expected: 0},
        "another code ignored": {values: map[string]string{melodyconfig.BootWarningsIgnoredKey: "security.firewallDoorReachUnknown"}, expected: 2},
    } {
        runtimeInstance, logger := newPlaintextAddressRuntime(t, t.Context(), testCase.values)
        (&plaintextAddressJudge{}).judge(runtimeInstance, []string{"redis.example.com:6379", "replica.example.com:6379"})

        if testCase.expected != len(plaintextAddressWarnings(logger)) {
            t.Fatalf("%s: expected %d warnings, got %+v", name, testCase.expected, plaintextAddressWarnings(logger))
        }
    }
}

/* the once is the provider's: a second application's provider over the same address writes its own line */
func TestProvider_JudgesEachAddressOncePerProvider(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance, logger := newPlaintextAddressRuntime(t, cancelledContext, nil)

    for _, provider := range []*Provider{NewProvider("melody", ProviderConfig{}), NewProvider("melody", ProviderConfig{})} {
        for range 2 {
            _, _ = provider.Open(runtimeInstance, ConnectionParameters{Address: "redis.example.com:6379"})
        }
    }

    if 2 != len(plaintextAddressWarnings(logger)) {
        t.Fatalf("expected one warning per provider, got %+v", plaintextAddressWarnings(logger))
    }
}

/* the runtime's context is done, so nothing is dialled; the warning is written first, and never with tls configured */
func TestProviderOpen_WritesThePlaintextAddressWarningWithoutTls(t *testing.T) {
    for name, testCase := range map[string]struct {
        client   ClientConfig
        expected int
    }{
        "without tls": {client: ClientConfig{}, expected: 1},
        "with tls":    {client: ClientConfig{TlsConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, expected: 0},
    } {
        cancelledContext, cancel := context.WithCancel(context.Background())
        cancel()
        runtimeInstance, logger := newPlaintextAddressRuntime(t, cancelledContext, nil)

        _, openErr := NewProvider("melody", ProviderConfig{Client: testCase.client}).Open(runtimeInstance, ConnectionParameters{Address: "redis.example.com:6379", Password: "secret"})
        if nil == openErr {
            t.Fatalf("%s: expected the open refused under a done context", name)
        }

        warnings := plaintextAddressWarnings(logger)
        if testCase.expected != len(warnings) {
            t.Fatalf("%s: expected %d warnings, got %+v", name, testCase.expected, warnings)
        }

        for _, warning := range warnings {
            if true == strings.Contains(fmt.Sprint(warning.context), "secret") {
                t.Fatalf("%s: expected no credential in the warning, got %v", name, warning.context)
            }
        }
    }
}

type plaintextAddressBootWarningRegistrar struct {
    declared []string
}

func (instance *plaintextAddressBootWarningRegistrar) DeclareBootWarning(name string, config applicationcontract.BootWarningConfig) {
    instance.declared = append(instance.declared, name)
}

func (instance *plaintextAddressBootWarningRegistrar) AddBootWarning(warning applicationcontract.BootWarning) {
}

func TestModule_RegisterBootWarningsDeclaresThePlaintextAddressCode(t *testing.T) {
    registrar := &plaintextAddressBootWarningRegistrar{}
    NewModule(ModuleConfig{}).RegisterBootWarnings(registrar)

    if 1 != len(registrar.declared) || PlaintextAddressBootWarningName != registrar.declared[0] {
        t.Fatalf("expected the module to declare %s, got %v", PlaintextAddressBootWarningName, registrar.declared)
    }
}
