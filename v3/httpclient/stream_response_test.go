package httpclient

import (
    "io"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestHttpClientRequestStream_ReturnsBodyAndCanBeClosed(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.WriteHeader(200)
        _, _ = writer.Write([]byte("stream"))
    }))
    defer server.Close()

    client := NewHttpClient(NewHttpClientConfig(server.URL, 0, nil))

    streamResponse, err := client.RequestStream(http.MethodGet, "/")
    if nil != err {
        t.Fatalf("request error: %v", err)
    }
    if 200 != streamResponse.StatusCode() {
        t.Fatalf("expected status 200")
    }

    bodyBytes, err := io.ReadAll(streamResponse.Body())
    if nil != err {
        t.Fatalf("read error: %v", err)
    }
    if "stream" != string(bodyBytes) {
        t.Fatalf("unexpected body")
    }

    err = streamResponse.Close()
    if nil != err {
        t.Fatalf("close error: %v", err)
    }
}
