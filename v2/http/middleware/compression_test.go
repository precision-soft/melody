package middleware

import (
    "bytes"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type failingReader struct {
    data      []byte
    readIndex int
    failAfter int
}

func newFailingReader(data string, failAfter int) *failingReader {
    return &failingReader{
        data:      []byte(data),
        readIndex: 0,
        failAfter: failAfter,
    }
}

func (instance *failingReader) Read(p []byte) (int, error) {
    if instance.readIndex >= instance.failAfter {
        return 0, errors.New("simulated read error")
    }

    remaining := instance.data[instance.readIndex:]
    n := copy(p, remaining)
    instance.readIndex += n

    if instance.readIndex >= instance.failAfter {
        return n, errors.New("simulated read error")
    }

    if instance.readIndex >= len(instance.data) {
        return n, io.EOF
    }

    return n, nil
}

func TestCompressionMiddleware_ReadAllError_PreservesPartialData(t *testing.T) {
    config := NewCompressionConfig(
        6,
        0,
        nil,
        nil,
    )

    middleware := CompressionMiddleware(config)

    partialBody := strings.Repeat("a", 100)
    reader := newFailingReader(partialBody, 50)

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        headers := make(nethttp.Header)
        headers.Set("Content-Type", "text/plain")

        return &http.Response{}, nil
    }

    _ = next

    response := &http.Response{}
    response.SetStatusCode(200)
    responseHeaders := make(nethttp.Header)
    responseHeaders.Set("Content-Type", "text/plain")
    response.SetHeaders(responseHeaders)
    response.SetBodyReader(reader)

    handler := middleware(
        func(
            runtimeInstance runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            return response, nil
        },
    )

    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.Header.Set("Accept-Encoding", "gzip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)

    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if nil == resultResponse {
        t.Fatalf("expected non-nil response")
    }

    bodyReader := resultResponse.BodyReader()
    if nil == bodyReader {
        t.Fatalf("expected non-nil body reader after read error")
    }

    bodyBytes, readErr := io.ReadAll(bodyReader)
    if nil != readErr {
        t.Fatalf("expected body reader to be readable, got error: %v", readErr)
    }

    if 0 == len(bodyBytes) {
        t.Fatalf("expected body to contain partial data, got empty")
    }
}

func TestCompressionMiddleware_SuccessfulCompression(t *testing.T) {
    config := NewCompressionConfig(
        6,
        10,
        nil,
        nil,
    )

    middleware := CompressionMiddleware(config)

    body := strings.Repeat("hello world ", 200)

    handler := middleware(
        func(
            runtimeInstance runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            response := &http.Response{}
            response.SetStatusCode(200)
            responseHeaders := make(nethttp.Header)
            responseHeaders.Set("Content-Type", "text/plain")
            response.SetHeaders(responseHeaders)
            response.SetBodyReader(bytes.NewReader([]byte(body)))

            return response, nil
        },
    )

    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.Header.Set("Accept-Encoding", "gzip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)

    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if nil == resultResponse {
        t.Fatalf("expected non-nil response")
    }

    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected gzip content-encoding, got: %s", resultResponse.Headers().Get("Content-Encoding"))
    }
}

func TestCompressionMiddleware_SkipsWhenBelowMinSize(t *testing.T) {
    config := NewCompressionConfig(
        6,
        10000,
        nil,
        nil,
    )

    middleware := CompressionMiddleware(config)

    handler := middleware(
        func(
            runtimeInstance runtimecontract.Runtime,
            writer nethttp.ResponseWriter,
            request httpcontract.Request,
        ) (httpcontract.Response, error) {
            response := &http.Response{}
            response.SetStatusCode(200)
            responseHeaders := make(nethttp.Header)
            responseHeaders.Set("Content-Type", "text/plain")
            response.SetHeaders(responseHeaders)
            response.SetBodyReader(bytes.NewReader([]byte("small")))

            return response, nil
        },
    )

    req := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    req.Header.Set("Accept-Encoding", "gzip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)

    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if "" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected no content-encoding for small body")
    }
}
