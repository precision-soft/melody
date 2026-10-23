package httpclient

import (
    "errors"
    "net"
    "net/http"
    "net/http/httptest"
    "sync/atomic"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func newTestHttpClient(t *testing.T, config HttpClientConfig) *HttpClient {
    t.Helper()

    client, err := NewHttpClient(config)
    if nil != err {
        t.Fatalf("unexpected configuration refusal: %v", err)
    }

    t.Cleanup(func() {
        _ = client.Close()
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
