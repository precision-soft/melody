package otlp

import (
    "bytes"
    "context"
    "strings"
    "testing"

    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func TestCollectorIsLocal_AdmitsLocalhostAndLoopbackOnly(t *testing.T) {
    for endpoint, expected := range map[string]bool{
        "localhost:4317":      true,
        "LOCALHOST":           true,
        "127.0.0.1:4317":      true,
        "[::1]:4317":          true,
        "otel-collector:4317": false,
        "10.0.0.5:4317":       false,
        "collector.example":   false,
    } {
        if expected != collectorIsLocal(endpoint) {
            t.Fatalf("collectorIsLocal(%q): expected %v", endpoint, expected)
        }
    }
}

func TestInsecureCollectorWarningOf_NamesAnInsecureExportOffThisMachineOnly(t *testing.T) {
    for _, testCase := range []struct {
        endpoint string
        insecure bool
        warns    bool
    }{
        {"otel-collector:4317", true, true},
        {"otel-collector:4317", false, false},
        {"127.0.0.1:4317", true, false},
        {"", true, false},
    } {
        warning, warns := insecureCollectorWarningOf(testCase.endpoint, Config{Insecure: testCase.insecure})
        if testCase.warns != warns {
            t.Fatalf("%q insecure=%v: expected warns=%v, got %v", testCase.endpoint, testCase.insecure, testCase.warns, warns)
        }

        if true == warns && (InsecureCollectorBootWarningName != warning.Name || insecureCollectorWarningMessage != warning.Message || testCase.endpoint != warning.Context["endpoint"] || 1 != len(warning.Context)) {
            t.Fatalf("expected the warning naming the endpoint alone, got %v", warning)
        }
    }
}

type insecureCollectorEnvironmentSource struct {
    values map[string]string
}

func (instance *insecureCollectorEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

/* a runtime whose context is done, so the provider is refused before the dial, with a json journal and, for values, a configuration */
func insecureCollectorRuntime(t *testing.T, values map[string]string) (containercontract.Runtime, *bytes.Buffer) {
    t.Helper()

    journal := &bytes.Buffer{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[loggingcontract.Logger](serviceContainer, logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logging.NewJsonLogger(journal, logging.JsonLoggerConfig{}), nil
    }, container.RegisterOptions{})

    if nil != values {
        environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &insecureCollectorEnvironmentSource{values: values})
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

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    return runtime.NewRun(doneContext, serviceContainer.NewScope()), journal
}

/* a provider built by hand writes the warning in the runtime's journal, as the module writes it at boot; the module's own build writes it once, as the boot warning */
func TestNewTracerProvider_JournalsAnInsecureExportOffThisMachine(t *testing.T) {
    for name, testCase := range map[string]struct {
        endpoint string
        config   Config
        values   map[string]string
        build    func(containercontract.Runtime, string, Config) error
        expected int
    }{
        "by hand, insecure off this machine": {endpoint: "collector.internal:4317", config: Config{Insecure: true}, expected: 1},
        "by hand, the code ignored":          {endpoint: "collector.internal:4317", config: Config{Insecure: true}, values: map[string]string{melodyconfig.BootWarningsIgnoredKey: InsecureCollectorBootWarningName}, expected: 0},
        "by hand, another code ignored":      {endpoint: "collector.internal:4317", config: Config{Insecure: true}, values: map[string]string{melodyconfig.BootWarningsIgnoredKey: "security.firewallDoorReachUnknown"}, expected: 1},
        "by hand, loopback":                  {endpoint: "127.0.0.1:4317", config: Config{Insecure: true}, expected: 0},
        "by hand, secure":                    {endpoint: "collector.internal:4317", config: Config{}, expected: 0},
        "the module's build": {endpoint: "collector.internal:4317", config: Config{Insecure: true}, build: func(runtimeInstance containercontract.Runtime, endpoint string, config Config) error {
            _, buildErr := newTracerProvider(runtimeInstance, endpoint, config)

            return buildErr
        }, expected: 0},
    } {
        runtimeInstance, journal := insecureCollectorRuntime(t, testCase.values)

        build := testCase.build
        if nil == build {
            build = func(runtimeInstance containercontract.Runtime, endpoint string, config Config) error {
                _, buildErr := NewTracerProvider(runtimeInstance, endpoint, config)

                return buildErr
            }
        }

        if buildErr := build(runtimeInstance, testCase.endpoint, testCase.config); nil == buildErr {
            t.Fatalf("%s: expected the build refused under a done context", name)
        }

        if testCase.expected != strings.Count(journal.String(), InsecureCollectorBootWarningName) {
            t.Fatalf("%s: expected %d warnings, got %q", name, testCase.expected, journal.String())
        }

        if 0 < testCase.expected && false == strings.Contains(journal.String(), "collector.internal:4317") {
            t.Fatalf("%s: expected the warning to name the endpoint, got %q", name, journal.String())
        }
    }
}
