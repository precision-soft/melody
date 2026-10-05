package awss3

import (
    "context"
    "sync"
    "testing"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* warningCapturingLogger keeps the context of every warning */
type warningCapturingLogger struct {
    mutex    sync.Mutex
    warnings []loggingcontract.Context
}

func (instance *warningCapturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.warnings = append(instance.warnings, context)
}

func (instance *warningCapturingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) Emergency(message string, context loggingcontract.Context) {}

func (instance *warningCapturingLogger) captured() []loggingcontract.Context {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]loggingcontract.Context{}, instance.warnings...)
}

func newRuntimeWithLogger(logger loggingcontract.Logger) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    })

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
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
    logger := &warningCapturingLogger{}
    runtimeInstance := newRuntimeWithLogger(logger)

    storage := NewStorage(newClientFor(t, "s3.example.com", false), "bucket")

    for range 2 {
        _, _ = storage.PresignedUrl(runtimeInstance, "object.txt", 0)
    }

    warnings := logger.captured()
    if 1 != len(warnings) || plaintextEndpointBootWarningName != warnings[0][bootWarningContextKey] || "s3.example.com" != warnings[0]["endpoint"] {
        t.Fatalf("expected one warning naming the endpoint, got %v", warnings)
    }

    for key, value := range warnings[0] {
        if "secret" == value || "access" == value {
            t.Fatalf("the warning carried a credential under %q", key)
        }
    }
}

func TestNewStorage_ALoopbackPlaintextEndpointAndAnHttpsEndpointWriteNoWarning(t *testing.T) {
    for _, client := range []*minio.Client{newClientFor(t, "localhost:4566", false), newClientFor(t, "s3.example.com", true)} {
        logger := &warningCapturingLogger{}

        _, _ = NewStorage(client, "bucket").PresignedUrl(newRuntimeWithLogger(logger), "object.txt", 0)

        if 0 != len(logger.captured()) {
            t.Fatalf("expected no warning over %s, got %v", client.EndpointURL(), logger.captured())
        }
    }
}

func TestNewStorage_ASingleLabelPlaintextEndpointWarns(t *testing.T) {
    logger := &warningCapturingLogger{}

    _, _ = NewStorage(newClientFor(t, "localstack:4566", false), "bucket").PresignedUrl(newRuntimeWithLogger(logger), "object.txt", 0)

    if 1 != len(logger.captured()) {
        t.Fatalf("expected the single-label endpoint warned of, got %v", logger.captured())
    }
}

func TestConfig_SecureZeroValueBuildsAnHttpClientAndTrueAnHttpsOne(t *testing.T) {
    for secure, scheme := range map[bool]string{false: "http", true: "https"} {
        client, clientErr := NewClient(Config{Endpoint: "s3.example.com", AccessKey: "access", SecretKey: "secret", Secure: secure})
        if nil != clientErr {
            t.Fatalf("client: %v", clientErr)
        }

        if scheme != client.EndpointURL().Scheme {
            t.Fatalf("expected Secure=%v to build an %s client, got %s", secure, scheme, client.EndpointURL().Scheme)
        }
    }
}
