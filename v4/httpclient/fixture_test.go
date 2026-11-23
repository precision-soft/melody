package httpclient

import (
    "context"
    "errors"
    "net"
    "net/http"
    "net/http/httptest"
    "sync/atomic"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

func testRuntime() containercontract.Runtime {
    return testRuntimeUnder(context.Background())
}

func testRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

/* contextlessTestRuntime carries no context, which no constructor of the runtime package builds */
type contextlessTestRuntime struct {
    containercontract.Runtime
}

func (instance *contextlessTestRuntime) Context() context.Context {
    return nil
}

func newTestHttpClient(t *testing.T, config HttpClientConfig) *HttpClient {
    t.Helper()

    client, err := NewHttpClient(config)
    if nil != err {
        t.Fatalf("unexpected configuration refusal: %v", err)
    }

    t.Cleanup(func() {
        _ = client.CloseWithContext(context.Background())
    })

    return client
}

func newCountingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
    t.Helper()

    hits := &atomic.Int64{}

    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        hits.Add(1)
        writer.WriteHeader(http.StatusOK)
    }))
    t.Cleanup(server.Close)

    return server, hits
}

func assertRefusedField(t *testing.T, err error, field string, message string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(err, &refusal) {
        t.Fatalf("expected a melody error refusing %q, got %v", field, err)
    }

    if message != refusal.Message() {
        t.Fatalf("expected %q, got %q", message, refusal.Message())
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the refusal to name %q, got %+v", field, refusal.Context())
    }
}

func assertTimedOut(t *testing.T, err error) {
    t.Helper()

    var timeoutErr net.Error
    if false == errors.As(err, &timeoutErr) || false == timeoutErr.Timeout() {
        t.Fatalf("expected the exchange to time out, got %v", err)
    }
}

/* recordingRoundTripper keeps every request the chain hands it before passing it on */
type recordingRoundTripper struct {
    next     http.RoundTripper
    requests []*http.Request
}

func (instance *recordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
    instance.requests = append(instance.requests, request)

    return instance.next.RoundTrip(request)
}

func recordingDecorator(recorder *recordingRoundTripper) RoundTripperDecorator {
    return func(next http.RoundTripper) http.RoundTripper {
        recorder.next = next

        return recorder
    }
}

type labelRoundTripper struct {
    next  http.RoundTripper
    label string
    order *[]string
}

func (instance *labelRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
    *instance.order = append(*instance.order, instance.label)

    return instance.next.RoundTrip(request)
}

func labelDecorator(label string, order *[]string) RoundTripperDecorator {
    return func(next http.RoundTripper) http.RoundTripper {
        return &labelRoundTripper{next: next, label: label, order: order}
    }
}

type headerRoundTripper struct {
    next  http.RoundTripper
    name  string
    value string
}

func (instance *headerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
    cloned := request.Clone(request.Context())
    cloned.Header.Set(instance.name, instance.value)

    return instance.next.RoundTrip(cloned)
}

func headerDecorator(name string, value string) RoundTripperDecorator {
    return func(next http.RoundTripper) http.RoundTripper {
        return &headerRoundTripper{next: next, name: name, value: value}
    }
}

func assertRefusedIndex(t *testing.T, err error, index int) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(err, &refusal) {
        t.Fatalf("expected a melody error, got %v", err)
    }

    if index != refusal.Context()["index"] {
        t.Fatalf("expected the refusal to name index %d, got %+v", index, refusal.Context())
    }
}
