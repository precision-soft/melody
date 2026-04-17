package middleware

import (
    "bytes"
    "compress/gzip"
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

func TestCompressionMiddleware_ReadAllError_ReturnsError(t *testing.T) {
    config := NewCompressionConfig(
        6,
        0,
        nil,
        nil,
    )

    middleware := CompressionMiddleware(config)

    partialBody := strings.Repeat("a", 100)
    reader := newFailingReader(partialBody, 50)

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

    _, err := handler(nil, httptest.NewRecorder(), melodyRequest)

    if nil == err {
        t.Fatalf("expected non-nil error when body read fails, got nil")
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

func TestCompressionMiddleware_LevelZero_PreservesNoCompression(t *testing.T) {
    config := NewCompressionConfig(
        0,
        10,
        nil,
        nil,
    )

    _ = CompressionMiddleware(config)

    if 0 != config.Level() {
        t.Fatalf("expected level 0 (gzip.NoCompression) to be preserved, got %d", config.Level())
    }
}

func TestCompressionMiddleware_ExcludedPath_SkipsCompression(t *testing.T) {
    config := NewCompressionConfig(
        6,
        10,
        nil,
        []string{"/static/"},
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

    req := httptest.NewRequest(nethttp.MethodGet, "/static/file.js", nil)
    req.Header.Set("Accept-Encoding", "gzip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(req)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)

    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if "" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected no content-encoding for excluded path")
    }
}

func TestCompressionMiddleware_AlreadyEncoded_SkipsCompression(t *testing.T) {
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
            responseHeaders.Set("Content-Encoding", "br")
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

    if "br" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected original content-encoding to be preserved")
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

func TestCompressionMiddleware_LevelAcceptsHuffmanOnlyBound(t *testing.T) {
    config := NewCompressionConfig(
        gzip.HuffmanOnly,
        10,
        nil,
        nil,
    )

    CompressionMiddleware(config)

    if gzip.HuffmanOnly != config.Level() {
        t.Fatalf("expected HuffmanOnly level preserved, got %d", config.Level())
    }
}

func TestCompressionMiddleware_LevelAcceptsBestCompressionBound(t *testing.T) {
    config := NewCompressionConfig(
        gzip.BestCompression,
        10,
        nil,
        nil,
    )

    CompressionMiddleware(config)

    if gzip.BestCompression != config.Level() {
        t.Fatalf("expected BestCompression level preserved, got %d", config.Level())
    }
}

func TestCompressionMiddleware_LevelBelowHuffmanOnlyFallsBackToDefault(t *testing.T) {
    config := NewCompressionConfig(
        gzip.HuffmanOnly-1,
        10,
        nil,
        nil,
    )

    CompressionMiddleware(config)

    if gzip.DefaultCompression != config.Level() {
        t.Fatalf("expected default level when below HuffmanOnly, got %d", config.Level())
    }
}

func TestCompressionMiddleware_LevelAboveBestCompressionFallsBackToDefault(t *testing.T) {
    config := NewCompressionConfig(
        gzip.BestCompression+1,
        10,
        nil,
        nil,
    )

    CompressionMiddleware(config)

    if gzip.DefaultCompression != config.Level() {
        t.Fatalf("expected default level when above BestCompression, got %d", config.Level())
    }
}
