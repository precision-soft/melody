package amqp

import (
    "context"
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

type plaintextDsnEnvironmentSource struct {
    values map[string]string
}

func (instance *plaintextDsnEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

/* the judged hosts live for the process, so each test starts from none */
func forgetJudgedPlaintextHosts(t *testing.T) {
    t.Helper()

    judgedPlaintextHosts.Clear()
    t.Cleanup(judgedPlaintextHosts.Clear)
}

func newPlaintextDsnRuntime(t *testing.T, ctx context.Context, values map[string]string) (containercontract.Runtime, *recordingLogger) {
    t.Helper()

    logger := &recordingLogger{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    container.MustRegister[loggingcontract.Logger](serviceContainer, logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{})

    if nil != values {
        environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &plaintextDsnEnvironmentSource{values: values})
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

func plaintextDsnWarnings(logger *recordingLogger) []logRecord {
    logger.mutex.Lock()
    defer logger.mutex.Unlock()

    warnings := []logRecord{}
    for _, record := range logger.records {
        if PlaintextDsnBootWarningName == record.context[bootWarningContextKey] {
            warnings = append(warnings, record)
        }
    }

    return warnings
}

func TestPlaintextHostOf_OnlyAnAmqpDsnOffThisMachineIsNamed(t *testing.T) {
    for dsn, expected := range map[string]string{
        "amqp://guest:secret@broker.example.com:5672/": "broker.example.com:5672",
        "amqp://rabbitmq:5672/":                        "rabbitmq:5672",
        "amqp://10.0.0.5/":                             "10.0.0.5",
        "AMQP://broker.example.com/":                   "broker.example.com",
        "amqps://guest:secret@broker.example.com/":     "",
        "amqp://guest:secret@localhost:5672/":          "",
        "amqp://127.0.0.1:5672/":                       "",
        "amqp://[::1]:5672/":                           "",
        "not a dsn":                                    "",
    } {
        if expected != plaintextHostOf(dsn) {
            t.Fatalf("%s: expected %q, got %q", dsn, expected, plaintextHostOf(dsn))
        }
    }
}

func TestWarnPlaintextDsn_NamesTheHostOnceAndNeverTheCredential(t *testing.T) {
    forgetJudgedPlaintextHosts(t)

    runtimeInstance, logger := newPlaintextDsnRuntime(t, t.Context(), nil)
    for range 2 {
        warnPlaintextDsn(runtimeInstance, "amqp://guest:secret@broker.example.com:5672/")
        warnPlaintextDsn(runtimeInstance, "amqps://guest:secret@other.example.com/")
        warnPlaintextDsn(runtimeInstance, "amqp://guest:secret@localhost/")
    }

    warnings := plaintextDsnWarnings(logger)
    if 1 != len(warnings) || plaintextDsnWarningMessage != warnings[0].message || "broker.example.com:5672" != warnings[0].context["host"] {
        t.Fatalf("expected one warning naming the host, got %+v", warnings)
    }

    if true == strings.Contains(fmt.Sprint(warnings[0].context), "secret") {
        t.Fatalf("expected the warning to carry no credential, got %v", warnings[0].context)
    }
}

func TestWarnPlaintextDsn_HonoursTheBootWarningIgnoreKey(t *testing.T) {
    for name, testCase := range map[string]struct {
        values   map[string]string
        expected int
    }{
        "no key":               {values: map[string]string{}, expected: 1},
        "the code ignored":     {values: map[string]string{melodyconfig.BootWarningsIgnoredKey: PlaintextDsnBootWarningName}, expected: 0},
        "another code ignored": {values: map[string]string{melodyconfig.BootWarningsIgnoredKey: "security.firewallDoorReachUnknown"}, expected: 1},
    } {
        forgetJudgedPlaintextHosts(t)

        runtimeInstance, logger := newPlaintextDsnRuntime(t, t.Context(), testCase.values)
        warnPlaintextDsn(runtimeInstance, "amqp://broker.example.com/")

        if testCase.expected != len(plaintextDsnWarnings(logger)) {
            t.Fatalf("%s: expected %d warnings, got %+v", name, testCase.expected, plaintextDsnWarnings(logger))
        }
    }
}

/* the runtime's context is done, so the dial never leaves; the warning is written first all the same */
func TestProviderOpen_WritesThePlaintextDsnWarning(t *testing.T) {
    forgetJudgedPlaintextHosts(t)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance, logger := newPlaintextDsnRuntime(t, cancelledContext, nil)

    if _, openErr := NewProvider("melody", ProviderConfig{}).Open(runtimeInstance, "amqp://guest:secret@broker.example.com:5672/"); nil == openErr {
        t.Fatalf("expected the open refused under a done context")
    }

    if 1 != len(plaintextDsnWarnings(logger)) {
        t.Fatalf("expected the open to name the plaintext dsn, got %+v", plaintextDsnWarnings(logger))
    }
}

type plaintextDsnBootWarningRegistrar struct {
    declared []string
}

func (instance *plaintextDsnBootWarningRegistrar) DeclareBootWarning(name string, config applicationcontract.BootWarningConfig) {
    instance.declared = append(instance.declared, name)
}

func (instance *plaintextDsnBootWarningRegistrar) AddBootWarning(warning applicationcontract.BootWarning) {
}

func TestModule_RegisterBootWarningsDeclaresThePlaintextDsnCode(t *testing.T) {
    registrar := &plaintextDsnBootWarningRegistrar{}
    NewModule(ModuleConfig{}).RegisterBootWarnings(registrar)

    if 1 != len(registrar.declared) || PlaintextDsnBootWarningName != registrar.declared[0] {
        t.Fatalf("expected the module to declare %s, got %v", PlaintextDsnBootWarningName, registrar.declared)
    }
}
