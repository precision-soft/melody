package awss3

import (
    "context"
    "encoding/xml"
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
)

func TestNewClientRefusesAnEmptyAccessKey(t *testing.T) {
    _, clientErr := NewClient(Config{Endpoint: "localhost:9000", AccessKey: "", SecretKey: "secret"})

    if nil == clientErr {
        t.Fatal("expected an empty access key to be refused rather than silently downgrading to anonymous access")
    }

    if false == strings.Contains(clientErr.Error(), "anonymous") {
        t.Fatalf("expected the refusal to name the anonymous downgrade, got %v", clientErr)
    }
}

func TestNewClientRefusesAnEmptySecretKey(t *testing.T) {
    _, clientErr := NewClient(Config{Endpoint: "localhost:9000", AccessKey: "access", SecretKey: ""})

    if nil == clientErr {
        t.Fatal("expected an empty secret key to be refused rather than silently downgrading to anonymous access")
    }
}

func TestNewClientAcceptsCompleteCredentials(t *testing.T) {
    client, clientErr := NewClient(Config{Endpoint: "localhost:9000", AccessKey: "access", SecretKey: "secret"})

    if nil != clientErr {
        t.Fatalf("expected complete credentials to build a client, got %v", clientErr)
    }

    if nil == client {
        t.Fatal("expected a client")
    }
}

/* bucketRaceServer plays an s3 endpoint mid bucket-creation race: the existence probe answers absent until the creation was attempted, and the creation itself answers the conflict a losing replica sees. */
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

    client, clientErr := NewClient(Config{
        Endpoint:  strings.TrimPrefix(server.URL, "http://"),
        AccessKey: "access",
        SecretKey: "secret",
    })
    if nil != clientErr {
        t.Fatalf("could not build the client: %v", clientErr)
    }

    if ensureErr := EnsureBucket(context.Background(), client, "raced-bucket", ""); nil != ensureErr {
        t.Fatalf("expected the lost creation race to read as success once the bucket is usable, got %v", ensureErr)
    }
}

/* aloneConflictServer answers the creation conflict but keeps reporting the bucket absent, the shape of BucketAlreadyExists on a name another account owns: the re-check must NOT excuse that refusal. */
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

    client, clientErr := NewClient(Config{
        Endpoint:  strings.TrimPrefix(server.URL, "http://"),
        AccessKey: "access",
        SecretKey: "secret",
    })
    if nil != clientErr {
        t.Fatalf("could not build the client: %v", clientErr)
    }

    if ensureErr := EnsureBucket(context.Background(), client, "foreign-bucket", ""); nil == ensureErr {
        t.Fatal("expected a conflict on a bucket the re-check cannot see to stay an error")
    }
}

func TestConfig_RedactsCredentialsOnEveryFmtVerb(t *testing.T) {
    config := Config{
        Endpoint:  "s3.example:9000",
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
        if false == strings.Contains(rendered, "s3.example:9000") {
            t.Fatalf("verb %s dropped the safe Endpoint field: %s", verb, rendered)
        }
    }

    pointerRendered := fmt.Sprintf("%v", &config)
    if true == strings.Contains(pointerRendered, "wJalrXUtn-the-secret") {
        t.Fatalf("a *Config leaked the secret key: %s", pointerRendered)
    }
}
