package awss3

import (
    "context"
    "strings"
    "sync"
    "testing"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* warningCapturingLogger keeps the message and the context of every warning */
type warningCapturingLogger struct {
    mutex    sync.Mutex
    warnings []loggingcontract.Context
    messages []string
}

func (instance *warningCapturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *warningCapturingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.warnings = append(instance.warnings, context)
    instance.messages = append(instance.messages, message)
}

func (instance *warningCapturingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) Emergency(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) captured() []loggingcontract.Context {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]loggingcontract.Context{}, instance.warnings...)
}

func newRuntimeWithLogger(logger loggingcontract.Logger) runtimecontract.Runtime {
    return newRuntimeOver(logger, nil)
}

/* a nil logger registers none, a nil configuration none, so each half of what the first-use writer reads can be left out */
func newRuntimeOver(logger loggingcontract.Logger, configuration configcontract.Configuration) runtimecontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    if nil != logger {
        serviceContainer.MustRegister(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        }, container.RegisterOptions{})
    }

    if nil != configuration {
        serviceContainer.MustRegister(melodyconfig.ServiceConfig, func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        }, container.RegisterOptions{})
    }

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

type testEnvironmentSource struct {
    values map[string]string
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func newTestConfiguration(t *testing.T, values map[string]string) configcontract.Configuration {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(&testEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("configuration: %v", configurationErr)
    }

    return configuration
}

/* the judged hosts live for the process, so each test starts from none */
func forgetJudgedPlaintextEndpoints(t *testing.T) {
    t.Helper()

    judgedPlaintextEndpoints.Clear()
    t.Cleanup(judgedPlaintextEndpoints.Clear)
}

func newClientFor(t *testing.T, endpoint string, secure bool) *minio.Client {
    t.Helper()

    client, clientErr := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4("access", "secret", ""), Secure: secure, Region: "us-east-1"})
    if nil != clientErr {
        t.Fatalf("client for %q: %v", endpoint, clientErr)
    }

    return client
}

func TestEndpointIsLocal_OnlyLocalhostAndLoopbackStaySilent(t *testing.T) {
    for endpoint, local := range map[string]bool{
        "127.0.0.1:4566":     true,
        "localhost:4566":     true,
        "LOCALHOST":          true,
        "[::1]:4566":         true,
        "::1":                true,
        "10.1.2.3:9000":      false,
        "172.16.0.9:9000":    false,
        "192.168.1.5":        false,
        "[fc00::1]:9000":     false,
        "localstack:4566":    false,
        "minio":              false,
        "s3.example.com":     false,
        "8.8.8.8:443":        false,
        "s3.amazonaws.com":   false,
        "s3.internal.corp:9": false,
    } {
        if local != endpointIsLocal(endpoint) {
            t.Fatalf("expected endpointIsLocal(%q) = %v", endpoint, local)
        }
    }
}

func TestNewStorage_APlaintextEndpointOffThisMachineIsNamedOnceAtTheFirstUse(t *testing.T) {
    forgetJudgedPlaintextEndpoints(t)

    logger := &warningCapturingLogger{}
    runtimeInstance := newRuntimeWithLogger(logger)

    storage := NewStorage(newClientFor(t, "s3.example.com", false), "bucket", StorageConfig{})

    for range 2 {
        _, _ = storage.PresignedUrl(runtimeInstance, "object.txt", 0)
    }

    warnings := logger.captured()
    if 1 != len(warnings) || PlaintextEndpointBootWarningName != warnings[0][bootWarningContextKey] || "s3.example.com" != warnings[0]["endpoint"] {
        t.Fatalf("expected one warning naming the endpoint, got %v", warnings)
    }

    for key, value := range warnings[0] {
        if "secret" == value || "access" == value {
            t.Fatalf("the warning carried a credential under %q", key)
        }
    }
}

func TestNewStorage_ALoopbackPlaintextEndpointAndAnHttpsEndpointWriteNoWarning(t *testing.T) {
    forgetJudgedPlaintextEndpoints(t)

    for _, client := range []*minio.Client{newClientFor(t, "localhost:4566", false), newClientFor(t, "s3.example.com", true)} {
        logger := &warningCapturingLogger{}

        _, _ = NewStorage(client, "bucket", StorageConfig{}).PresignedUrl(newRuntimeWithLogger(logger), "object.txt", 0)

        if 0 != len(logger.captured()) {
            t.Fatalf("expected no warning over %s, got %v", client.EndpointURL(), logger.captured())
        }
    }
}

func TestNewStorage_ASingleLabelPlaintextEndpointWarns(t *testing.T) {
    forgetJudgedPlaintextEndpoints(t)

    logger := &warningCapturingLogger{}

    _, _ = NewStorage(newClientFor(t, "localstack:4566", false), "bucket", StorageConfig{}).PresignedUrl(newRuntimeWithLogger(logger), "object.txt", 0)

    if 1 != len(logger.captured()) {
        t.Fatalf("expected the single-label endpoint warned of, got %v", logger.captured())
    }
}

func TestConfig_TheZeroValueBuildsAnHttpsClientAndInsecureAnHttpOne(t *testing.T) {
    for insecure, scheme := range map[bool]string{false: "https", true: "http"} {
        client, clientErr := NewClient(Config{Endpoint: "s3.example.com", AccessKey: "access", SecretKey: "secret", Insecure: insecure})
        if nil != clientErr {
            t.Fatalf("client: %v", clientErr)
        }

        if scheme != client.EndpointURL().Scheme {
            t.Fatalf("expected Insecure=%v to build an %s client, got %s", insecure, scheme, client.EndpointURL().Scheme)
        }
    }
}

func TestNewStorage_TheWarningNamesTheInsecureField(t *testing.T) {
    forgetJudgedPlaintextEndpoints(t)

    logger := &warningCapturingLogger{}

    _, _ = NewStorage(newClientFor(t, "s3.example.com", false), "bucket", StorageConfig{}).PresignedUrl(newRuntimeWithLogger(logger), "object.txt", 0)

    if 1 != len(logger.messages) || false == strings.Contains(logger.messages[0], "Config.Insecure") || true == strings.Contains(logger.messages[0], "Secure on") {
        t.Fatalf("expected the message to name Config.Insecure, got %v", logger.messages)
    }
}

func TestNewStorage_TwoStoragesOverOneEndpointNameItOnceAndAUseWithNoJournalLeavesItForTheNext(t *testing.T) {
    forgetJudgedPlaintextEndpoints(t)

    client := newClientFor(t, "s3.example.com", false)
    first := NewStorage(client, "bucket", StorageConfig{})
    second := NewStorage(client, "other", StorageConfig{})

    _, _ = first.PresignedUrl(newRuntimeOver(nil, nil), "object.txt", 0)

    logger := &warningCapturingLogger{}
    runtimeInstance := newRuntimeWithLogger(logger)

    _, _ = first.PresignedUrl(runtimeInstance, "object.txt", 0)
    _, _ = second.PresignedUrl(runtimeInstance, "object.txt", 0)

    if warnings := logger.captured(); 1 != len(warnings) {
        t.Fatalf("expected the endpoint named once across both storages, got %v", warnings)
    }
}

func TestNewStorage_TheFirstUseHonoursTheCliBootWarningKeysAndTheModulesRecord(t *testing.T) {
    for _, testCase := range []struct {
        name           string
        values         map[string]string
        judgedByModule bool
        expected       int
    }{
        {"no key", map[string]string{}, false, 1},
        {"the code ignored", map[string]string{melodyconfig.CliBootWarningsIgnoredKey: PlaintextEndpointBootWarningName}, false, 0},
        {"another code ignored", map[string]string{melodyconfig.CliBootWarningsIgnoredKey: "security.firewallDoorReachUnknown"}, false, 1},
        {"the journal disabled", map[string]string{melodyconfig.CliBootWarningsDisabledKey: "true"}, false, 0},
        {"already named by the module", map[string]string{}, true, 0},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            forgetJudgedPlaintextEndpoints(t)

            client := newClientFor(t, "s3.example.com", false)
            if true == testCase.judgedByModule {
                NewModule(ModuleConfig{Client: client, Bucket: "bucket"}).RegisterBootWarnings(&spyBootWarningRegistrar{})
            }

            logger := &warningCapturingLogger{}
            runtimeInstance := newRuntimeOver(logger, newTestConfiguration(t, testCase.values))

            for range 2 {
                _, _ = NewStorage(client, "bucket", StorageConfig{}).PresignedUrl(runtimeInstance, "object.txt", 0)
            }

            if warnings := logger.captured(); testCase.expected != len(warnings) {
                t.Fatalf("expected %d warnings, got %v", testCase.expected, warnings)
            }
        })
    }
}
