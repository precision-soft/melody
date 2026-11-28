package awss3

import (
    "context"
    "encoding/xml"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

func TestNewClientRefusesAnEmptyAccessKey(t *testing.T) {
    _, clientErr := NewClient("localhost:9000", Config{AccessKey: "", SecretKey: "secret"})

    if nil == clientErr {
        t.Fatal("expected an empty access key to be refused rather than silently downgrading to anonymous access")
    }

    if false == strings.Contains(clientErr.Error(), "anonymous") {
        t.Fatalf("expected the refusal to name the anonymous downgrade, got %v", clientErr)
    }
}

func TestNewClientRefusesAnEmptySecretKey(t *testing.T) {
    _, clientErr := NewClient("localhost:9000", Config{AccessKey: "access", SecretKey: ""})

    if nil == clientErr {
        t.Fatal("expected an empty secret key to be refused rather than silently downgrading to anonymous access")
    }
}

func TestNewClientAcceptsCompleteCredentials(t *testing.T) {
    client, clientErr := NewClient("localhost:9000", Config{AccessKey: "access", SecretKey: "secret"})

    if nil != clientErr {
        t.Fatalf("expected complete credentials to build a client, got %v", clientErr)
    }

    if nil == client {
        t.Fatal("expected a client")
    }
}

type bucketRaceServer struct {
    makeAttempted atomic.Bool
    conflictCode  string
}

func (instance *bucketRaceServer) ServeHTTP(writer nethttp.ResponseWriter, request *nethttp.Request) {
    /* minio's BucketExists probes with GET ?location= and reads 200 as "exists" */
    if nethttp.MethodGet == request.Method || nethttp.MethodHead == request.Method {
        if true == instance.makeAttempted.Load() {
            writer.WriteHeader(nethttp.StatusOK)
            _, _ = writer.Write([]byte(`<LocationConstraint></LocationConstraint>`))

            return
        }

        writer.WriteHeader(nethttp.StatusNotFound)

        return
    }

    if nethttp.MethodPut == request.Method {
        instance.makeAttempted.Store(true)

        writer.WriteHeader(nethttp.StatusConflict)

        type errorResponse struct {
            XMLName xml.Name `xml:"Error"`
            Code    string   `xml:"Code"`
            Message string   `xml:"Message"`
        }

        _ = xml.NewEncoder(writer).Encode(errorResponse{Code: instance.conflictCode, Message: "conflict"})

        return
    }

    writer.WriteHeader(nethttp.StatusNotImplemented)
}

func TestEnsureBucketTreatsALostCreationRaceAsSuccessWhenTheBucketIsUsable(t *testing.T) {
    handler := &bucketRaceServer{conflictCode: "BucketAlreadyOwnedByYou"}
    server := httptest.NewServer(handler)
    defer server.Close()

    client, clientErr := NewClient(strings.TrimPrefix(server.URL, "http://"), Config{
        AccessKey: "access",
        SecretKey: "secret",
        Insecure:  true,
    })
    if nil != clientErr {
        t.Fatalf("could not build the client: %v", clientErr)
    }

    if ensureErr := EnsureBucket(bucketTestRuntime(), client, "raced-bucket", EnsureBucketOptions{}); nil != ensureErr {
        t.Fatalf("expected the lost creation race to read as success once the bucket is usable, got %v", ensureErr)
    }
}

type aloneConflictServer struct{}

func (instance aloneConflictServer) ServeHTTP(writer nethttp.ResponseWriter, request *nethttp.Request) {
    if nethttp.MethodGet == request.Method || nethttp.MethodHead == request.Method {
        writer.WriteHeader(nethttp.StatusNotFound)

        return
    }

    if nethttp.MethodPut == request.Method {
        writer.WriteHeader(nethttp.StatusConflict)

        type errorResponse struct {
            XMLName xml.Name `xml:"Error"`
            Code    string   `xml:"Code"`
            Message string   `xml:"Message"`
        }

        _ = xml.NewEncoder(writer).Encode(errorResponse{Code: "BucketAlreadyExists", Message: "taken"})

        return
    }

    writer.WriteHeader(nethttp.StatusNotImplemented)
}

func TestEnsureBucketStillRefusesANameOwnedElsewhere(t *testing.T) {
    server := httptest.NewServer(aloneConflictServer{})
    defer server.Close()

    client, clientErr := NewClient(strings.TrimPrefix(server.URL, "http://"), Config{
        AccessKey: "access",
        SecretKey: "secret",
        Insecure:  true,
    })
    if nil != clientErr {
        t.Fatalf("could not build the client: %v", clientErr)
    }

    if ensureErr := EnsureBucket(bucketTestRuntime(), client, "foreign-bucket", EnsureBucketOptions{}); nil == ensureErr {
        t.Fatal("expected a conflict on a bucket the re-check cannot see to stay an error")
    }
}

func TestConfig_RedactsCredentialsOnEveryFmtVerb(t *testing.T) {
    config := Config{
        AccessKey: "AKIA-public-part",
        SecretKey: "wJalrXUtn-the-secret",
        Region:    "us-east-1",
    }

    for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d"} {
        rendered := fmt.Sprintf(verb, config)
        if true == strings.Contains(rendered, "AKIA-public-part") {
            t.Fatalf("verb %s leaked the access key: %s", verb, rendered)
        }
        if true == strings.Contains(rendered, "wJalrXUtn-the-secret") {
            t.Fatalf("verb %s leaked the secret key: %s", verb, rendered)
        }
        if false == strings.Contains(rendered, "us-east-1") {
            t.Fatalf("verb %s dropped the safe Region field: %s", verb, rendered)
        }
    }

    if rendered := fmt.Sprintf("%v", config); false == strings.HasPrefix(rendered, "aws.s3.Config{") {
        t.Fatalf("expected the rendering to carry the module's name, got %s", rendered)
    }

    pointerRendered := fmt.Sprintf("%v", &config)
    if true == strings.Contains(pointerRendered, "wJalrXUtn-the-secret") {
        t.Fatalf("a *Config leaked the secret key: %s", pointerRendered)
    }
}

type recheckFailingConflictServer struct {
    mutex  sync.Mutex
    checks int
}

func (instance *recheckFailingConflictServer) ServeHTTP(writer nethttp.ResponseWriter, request *nethttp.Request) {
    if nethttp.MethodPut == request.Method {
        writer.WriteHeader(nethttp.StatusConflict)

        type errorResponse struct {
            XMLName xml.Name `xml:"Error"`
            Code    string   `xml:"Code"`
            Message string   `xml:"Message"`
        }

        _ = xml.NewEncoder(writer).Encode(errorResponse{Code: "BucketAlreadyExists", Message: "taken"})

        return
    }

    instance.mutex.Lock()
    instance.checks++
    firstCheck := 1 == instance.checks
    instance.mutex.Unlock()

    if true == firstCheck {
        writer.WriteHeader(nethttp.StatusNotFound)

        return
    }

    writer.WriteHeader(nethttp.StatusInternalServerError)
}

func TestEnsureBucketNamesAReCheckThatCouldNotComplete(t *testing.T) {
    server := httptest.NewServer(&recheckFailingConflictServer{})
    defer server.Close()

    client, clientErr := NewClient(strings.TrimPrefix(server.URL, "http://"), Config{
        AccessKey: "access",
        SecretKey: "secret",
        Insecure:  true,
    })
    if nil != clientErr {
        t.Fatalf("could not build the client: %v", clientErr)
    }

    ensureErr := EnsureBucket(bucketTestRuntime(), client, "contested-bucket", EnsureBucketOptions{})
    if nil == ensureErr || false == strings.Contains(ensureErr.Error(), "the re-check of its existence could not complete") {
        t.Fatalf("expected the refusal to name the failed re-check, got %v", ensureErr)
    }
}

func TestNewClient_ARefusalNamesTheField(t *testing.T) {
    testCases := []struct {
        endpoint string
        config   Config
        key      string
        name     string
    }{
        {"", Config{AccessKey: "access", SecretKey: "secret"}, "argument", "endpoint"},
        {"localhost:9000", Config{SecretKey: "secret"}, "field", "Config.AccessKey"},
        {"localhost:9000", Config{AccessKey: "access"}, "field", "Config.SecretKey"},
    }

    for _, testCase := range testCases {
        _, clientErr := NewClient(testCase.endpoint, testCase.config)

        var refusal *exception.Error
        if false == errors.As(clientErr, &refusal) || testCase.name != refusal.Context()[testCase.key] {
            t.Fatalf("expected a refusal naming %s under %s, got %v", testCase.name, testCase.key, clientErr)
        }
    }
}

type regionRecordingServer struct {
    mutex sync.Mutex
    body  string
}

func (instance *regionRecordingServer) ServeHTTP(writer nethttp.ResponseWriter, request *nethttp.Request) {
    if nethttp.MethodPut != request.Method {
        writer.WriteHeader(nethttp.StatusNotFound)

        return
    }

    body, _ := io.ReadAll(request.Body)

    instance.mutex.Lock()
    instance.body = string(body)
    instance.mutex.Unlock()

    writer.WriteHeader(nethttp.StatusOK)
}

func TestEnsureBucket_TheConfiguredRegionReachesTheCreation(t *testing.T) {
    handler := &regionRecordingServer{}
    server := httptest.NewServer(handler)
    defer server.Close()

    client, clientErr := NewClient(strings.TrimPrefix(server.URL, "http://"), Config{
        AccessKey: "access",
        SecretKey: "secret",
        Insecure:  true,
    })
    if nil != clientErr {
        t.Fatalf("could not build the client: %v", clientErr)
    }

    if ensureErr := EnsureBucket(bucketTestRuntime(), client, "regional-bucket", EnsureBucketOptions{Region: "eu-west-1"}); nil != ensureErr {
        t.Fatalf("unexpected error: %v", ensureErr)
    }

    handler.mutex.Lock()
    defer handler.mutex.Unlock()

    if false == strings.Contains(handler.body, "eu-west-1") {
        t.Fatalf("expected the creation to carry the configured region, got %q", handler.body)
    }
}

func bucketTestRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}

func TestEnsureBucket_RefusesANilRuntime(t *testing.T) {
    if ensureErr := EnsureBucket(nil, nil, "any-bucket", EnsureBucketOptions{}); nil == ensureErr {
        t.Fatalf("expected a nil runtime to be refused")
    }
}

type phaseBlockingServer struct {
    blockPhase    string
    makeAttempted atomic.Bool
    released      chan struct{}
}

func (instance *phaseBlockingServer) ServeHTTP(writer nethttp.ResponseWriter, request *nethttp.Request) {
    if nethttp.MethodGet == request.Method || nethttp.MethodHead == request.Method {
        if true == instance.makeAttempted.Load() {
            if "recheck" == instance.blockPhase {
                <-instance.released
            }

            writer.WriteHeader(nethttp.StatusOK)

            return
        }

        if "check" == instance.blockPhase {
            <-instance.released
        }

        writer.WriteHeader(nethttp.StatusNotFound)

        return
    }

    instance.makeAttempted.Store(true)
    if "create" == instance.blockPhase {
        <-instance.released
    }

    writer.WriteHeader(nethttp.StatusConflict)

    type errorResponse struct {
        XMLName xml.Name `xml:"Error"`
        Code    string   `xml:"Code"`
        Message string   `xml:"Message"`
    }

    _ = xml.NewEncoder(writer).Encode(errorResponse{Code: "BucketAlreadyOwnedByYou", Message: "conflict"})
}

func TestEnsureBucket_EachOfItsThreeCallsEndsWithTheRuntimesContext(t *testing.T) {
    for _, phase := range []string{"check", "create", "recheck"} {
        handler := &phaseBlockingServer{blockPhase: phase, released: make(chan struct{})}
        server := httptest.NewServer(handler)

        client, clientErr := NewClient(strings.TrimPrefix(server.URL, "http://"), Config{AccessKey: "access", SecretKey: "secret", Insecure: true})
        if nil != clientErr {
            t.Fatalf("could not build the client: %v", clientErr)
        }

        deadlineContext, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
        runtimeInstance := runtime.NewRun(deadlineContext, container.NewContainer(container.ContainerConfig{}).NewScope())

        done := make(chan error, 1)
        go func() {
            done <- EnsureBucket(runtimeInstance, client, "phase-bucket", EnsureBucketOptions{})
        }()

        select {
        case ensureErr := <-done:
            if false == errors.Is(ensureErr, context.DeadlineExceeded) {
                t.Fatalf("%s: expected the call to end with the runtime's deadline, got %v", phase, ensureErr)
            }
        case <-time.After(5 * time.Second):
            t.Fatalf("%s: expected the call to end with the runtime's deadline", phase)
        }

        cancel()
        close(handler.released)
        server.Close()
    }
}
