package middleware

import (
    "bytes"
    "compress/gzip"
    "context"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "testing"
    "testing/iotest"
    "time"

    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
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

type closeSignalReader struct {
    reader     io.Reader
    closeOnce  sync.Once
    closedFlag chan struct{}
}

func newCloseSignalReader(data string) *closeSignalReader {
    return &closeSignalReader{
        reader:     bytes.NewReader([]byte(data)),
        closedFlag: make(chan struct{}),
    }
}

func (instance *closeSignalReader) Read(p []byte) (int, error) {
    return instance.reader.Read(p)
}

func (instance *closeSignalReader) Close() error {
    instance.closeOnce.Do(func() { close(instance.closedFlag) })
    return nil
}

/* When a middleware outer to compression panics after next() returned, the kernel dropped the pipe-backed response without closing it, so the gzip goroutine blocked forever in pipe.Write and its deferred close of the original body reader (and its descriptor) never ran; tying the pipe reader to request-context cancellation must release it. */
func TestCompressionMiddleware_ReleasesGzipGoroutineWhenRequestUnwinds(t *testing.T) {
    config := NewCompressionConfig(6, 16, nil, nil)
    middleware := CompressionMiddleware(config)

    originalReader := newCloseSignalReader(strings.Repeat("a", 64))

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
            response.SetBodyReader(originalReader)

            return response, nil
        },
    )

    requestContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil).WithContext(requestContext)
    request.Header.Set("Accept-Encoding", "gzip")

    resultResponse, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }
    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the response to be compressed, got encoding %q", resultResponse.Headers().Get("Content-Encoding"))
    }

    /* the pipe-backed body is deliberately never read, mirroring an outer-middleware panic that abandons the response; cancelling the request context must unblock the gzip goroutine so its deferred close of the original reader runs */
    cancel()

    select {
    case <-originalReader.closedFlag:
    case <-time.After(2 * time.Second):
        t.Fatalf("original body reader was never closed after the request unwound; the gzip goroutine leaked and pinned its descriptor")
    }
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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

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

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

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

/* The normalization lands on the middleware's private copy: the caller's own object keeps the value it was built with, and the served response proves the invalid level was replaced — a gzip writer over the raw invalid level could not produce the encoded body. */
func TestCompressionMiddleware_LevelBelowHuffmanOnlyFallsBackToDefaultWithoutMutatingTheCallersConfig(t *testing.T) {
    config := NewCompressionConfig(
        gzip.HuffmanOnly-1,
        10,
        nil,
        nil,
    )

    resultResponse := serveCompressibleBodyThrough(t, config)

    if gzip.HuffmanOnly-1 != config.Level() {
        t.Fatalf("expected the caller's config to keep its own level, got %d", config.Level())
    }

    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the normalized private copy to still serve gzip, got %q", resultResponse.Headers().Get("Content-Encoding"))
    }
}

func TestCompressionMiddleware_LevelAboveBestCompressionFallsBackToDefaultWithoutMutatingTheCallersConfig(t *testing.T) {
    config := NewCompressionConfig(
        gzip.BestCompression+1,
        10,
        nil,
        nil,
    )

    resultResponse := serveCompressibleBodyThrough(t, config)

    if gzip.BestCompression+1 != config.Level() {
        t.Fatalf("expected the caller's config to keep its own level, got %d", config.Level())
    }

    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the normalized private copy to still serve gzip, got %q", resultResponse.Headers().Get("Content-Encoding"))
    }
}

/* serveCompressibleBodyThrough runs one gzip-accepting request with a large compressible body through the middleware built from config and answers the resulting response. */
func serveCompressibleBodyThrough(t *testing.T, config *CompressionConfig) httpcontract.Response {
    t.Helper()

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
            response.SetBodyReader(bytes.NewReader([]byte(strings.Repeat("hello world ", 200))))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    resultResponse, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }
    if nil == resultResponse {
        t.Fatalf("expected non-nil response")
    }

    return resultResponse
}

func TestCompressionMiddleware_AddsVaryAcceptEncodingWhenCompressing(t *testing.T) {
    config := NewCompressionConfig(6, 10, nil, nil)
    middleware := CompressionMiddleware(config)

    body := strings.Repeat("melody ", 200)
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

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "Accept-Encoding" != resultResponse.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Accept-Encoding, got: %q", resultResponse.Headers().Get("Vary"))
    }
}

func TestCompressionMiddleware_AddsVaryEvenWhenClientRefusesGzip(t *testing.T) {
    config := NewCompressionConfig(6, 10, nil, nil)
    middleware := CompressionMiddleware(config)

    body := strings.Repeat("melody ", 200)
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

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "gzip;q=0, deflate")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "Accept-Encoding" != resultResponse.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Accept-Encoding when skipping due to client refusal, got: %q", resultResponse.Headers().Get("Vary"))
    }

    if "" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected no Content-Encoding when client refuses gzip, got: %q", resultResponse.Headers().Get("Content-Encoding"))
    }
}

func TestCompressionMiddleware_DoesNotDuplicateVary(t *testing.T) {
    config := NewCompressionConfig(6, 10, nil, nil)
    middleware := CompressionMiddleware(config)

    body := strings.Repeat("melody ", 200)
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
            responseHeaders.Add("Vary", "Accept-Encoding")
            response.SetHeaders(responseHeaders)
            response.SetBodyReader(bytes.NewReader([]byte(body)))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    varyValues := resultResponse.Headers().Values("Vary")
    if 1 != len(varyValues) {
        t.Fatalf("expected exactly one Vary entry, got: %v", varyValues)
    }
}

func TestCompressionMiddleware_StreamingGzipProducesValidOutput(t *testing.T) {
    config := NewCompressionConfig(6, 64, nil, nil)
    middleware := CompressionMiddleware(config)

    original := strings.Repeat("melody streaming ", 5000)

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
            response.SetBodyReader(bytes.NewReader([]byte(original)))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    gzipReader, readerErr := gzip.NewReader(resultResponse.BodyReader())
    if nil != readerErr {
        t.Fatalf("gzip.NewReader failed: %v", readerErr)
    }
    defer gzipReader.Close()

    decompressed, readAllErr := io.ReadAll(gzipReader)
    if nil != readAllErr {
        t.Fatalf("decompress failed: %v", readAllErr)
    }

    if original != string(decompressed) {
        t.Fatalf("round-trip mismatch: decompressed length=%d original length=%d", len(decompressed), len(original))
    }
}

func TestAcceptsGzip_Cases(t *testing.T) {
    cases := map[string]bool{
        "":                      false,
        "gzip":                  true,
        "GZIP":                  true,
        "gzip;q=1":              true,
        "gzip;q=0":              false,
        "gzip;Q=0":              false,
        "gzip;Q=1":              true,
        "*;Q=0":                 false,
        "gzip;q=0.0":            false,
        "deflate":               false,
        "*":                     true,
        "*;q=0":                 false,
        "gzip;q=0, *;q=1":       false,
        "*;q=0, gzip;q=0.5":     true,
        "deflate, gzip;q=0.001": true,
        "identity":              false,
    }

    for input, expected := range cases {
        got := acceptsGzip(input)
        if expected != got {
            t.Fatalf("acceptsGzip(%q) = %v, want %v", input, got, expected)
        }
    }
}

func TestAcceptsGzip_DropsAnEntryWhoseQualityFallsOutsideTheGrammar(t *testing.T) {
    cases := map[string]bool{
        "gzip;q=Inf":         false,
        "gzip;q=NaN":         false,
        "gzip;q=5":           false,
        "gzip;q=-1":          false,
        "gzip;q=abc":         false,
        "gzip;q=Inf, *":      true,
        "*;q=NaN, gzip":      true,
        "gzip;q=Inf, *;q=0":  false,
        "gzip;q=0.5, br;q=x": true,
    }

    for input, expected := range cases {
        got := acceptsGzip(input)
        if expected != got {
            t.Fatalf("acceptsGzip(%q) = %v, want %v", input, got, expected)
        }
    }
}

/* Only a zero minimum size was normalized, so a negative one reached make([]byte, peekSize) and panicked on every request through the middleware. */
func TestCompressionMiddleware_NegativeMinSizeIsNormalized(t *testing.T) {
    config := NewCompressionConfig(
        6,
        -1,
        nil,
        nil,
    )

    middleware := CompressionMiddleware(config)

    /* the caller's config keeps its own value; the request below succeeding is what proves the private copy was normalized, since the raw negative minimum made the peek arithmetic lie on every request */
    if -1 != config.MinSize() {
        t.Fatalf("expected the caller's config to keep its own minimum size, got %d", config.MinSize())
    }

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
            response.SetBodyReader(bytes.NewReader([]byte(strings.Repeat("hello world ", 200))))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    resultResponse, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }
    if nil == resultResponse {
        t.Fatalf("expected non-nil response")
    }
}

/* A response another layer already encoded is one of several encodings of its URL just as a gzip one is, so a shared cache that stored it under the URL alone would replay a brotli body to a client that never asked for brotli. The already-encoded early return sat above the Vary helper, which is exactly the negotiated case where the header decides correctness. */
func TestCompressionMiddleware_AddsVaryWhenResponseIsAlreadyEncoded(t *testing.T) {
    config := NewCompressionConfig(6, 10, nil, nil)
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
            responseHeaders.Set("Content-Encoding", "br")
            responseHeaders.Set("Cache-Control", "public, max-age=3600")
            response.SetHeaders(responseHeaders)
            response.SetBodyReader(bytes.NewReader([]byte(strings.Repeat("melody ", 200))))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "br, gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "Accept-Encoding" != resultResponse.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Accept-Encoding on an already-encoded response, got: %q", resultResponse.Headers().Get("Vary"))
    }
    if "br" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the existing encoding to be left alone, got: %q", resultResponse.Headers().Get("Content-Encoding"))
    }
}

/* An excluded path is still negotiated against Accept-Encoding by every other layer in front of it, so the response must still tell a cache that the URL has more than one representation. */
func TestCompressionMiddleware_AddsVaryOnExcludedPath(t *testing.T) {
    config := NewCompressionConfig(6, 10, nil, []string{"/assets"})
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
            response.SetBodyReader(bytes.NewReader([]byte(strings.Repeat("melody ", 200))))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/assets/app.js", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "Accept-Encoding" != resultResponse.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Accept-Encoding on an excluded path, got: %q", resultResponse.Headers().Get("Vary"))
    }
    if "" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected an excluded path to stay uncompressed, got: %q", resultResponse.Headers().Get("Content-Encoding"))
    }
}

/* An excluded content type is skipped because compressing it is pointless, not because the URL has a single representation; the header still has to be there. */
func TestCompressionMiddleware_AddsVaryOnExcludedContentType(t *testing.T) {
    config := NewCompressionConfig(6, 10, []string{"image/"}, nil)
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
            responseHeaders.Set("Content-Type", "image/png")
            response.SetHeaders(responseHeaders)
            response.SetBodyReader(bytes.NewReader([]byte(strings.Repeat("melody ", 200))))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/logo.png", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "Accept-Encoding" != resultResponse.Headers().Get("Vary") {
        t.Fatalf("expected Vary: Accept-Encoding on an excluded content type, got: %q", resultResponse.Headers().Get("Vary"))
    }
}

type stalledBodyReader struct {
    readCount int
}

/* Read answers the shape io.Reader permits but callers must not loop on: no bytes, no error, with room left in the destination. */
func (instance *stalledBodyReader) Read(p []byte) (int, error) {
    instance.readCount++

    return 0, nil
}

/* A body reader answering (0, nil) forever pinned the peek loop, and with it a request goroutine, at full processor for the lifetime of the process. io.Reader makes tolerating a zero-byte read the caller's obligation, so the loop has to give up and let the request fail. */
func TestCompressionMiddleware_StalledBodyReaderDoesNotSpin(t *testing.T) {
    config := NewCompressionConfig(6, 1024, nil, nil)
    middleware := CompressionMiddleware(config)

    reader := &stalledBodyReader{}

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
            response.SetBodyReader(reader)

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    handlerReturned := make(chan error, 1)
    go func() {
        _, err := handler(nil, httptest.NewRecorder(), melodyRequest)
        handlerReturned <- err
    }()

    select {
    case err := <-handlerReturned:
        if nil == err {
            t.Fatalf("expected a stuck body reader to fail the request, got a nil error")
        }
        if false == errors.Is(err, io.ErrNoProgress) {
            t.Fatalf("expected io.ErrNoProgress from a reader making no progress, got: %v", err)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("the peek loop never returned after %d reads; a body reader answering (0, nil) spins a request goroutine at full processor forever", reader.readCount)
    }
}

/* the two exclusion setters are the supported way to re-aim a compression policy after the defaults were taken, and neither had a test. Each keeps a copy, so a caller that reuses the slice it passed cannot rewrite the policy of a running middleware, and each reads an explicit nil as "exclude nothing" rather than leaving the previous list in place. */

func TestCompressionConfig_SetExcludedContentTypesKeepsACopyAndClearsOnNil(t *testing.T) {
    config := DefaultCompressionConfig()

    supplied := []string{"application/pdf"}
    config.SetExcludedContentTypes(supplied)

    supplied[0] = "text/html"

    excluded := config.ExcludedContentTypes()
    if 1 != len(excluded) || "application/pdf" != excluded[0] {
        t.Fatalf("expected the configuration to keep its own copy, got: %v", excluded)
    }

    excluded[0] = "text/plain"

    if "application/pdf" != config.ExcludedContentTypes()[0] {
        t.Fatalf("expected the accessor to hand out a copy as well")
    }

    config.SetExcludedContentTypes(nil)

    if nil != config.ExcludedContentTypes() {
        t.Fatalf("expected an explicit nil to clear the list, got: %v", config.ExcludedContentTypes())
    }
}

func TestCompressionConfig_SetExcludedPathsKeepsACopyAndClearsOnNil(t *testing.T) {
    config := DefaultCompressionConfig()

    supplied := []string{"/download"}
    config.SetExcludedPaths(supplied)

    supplied[0] = "/"

    excluded := config.ExcludedPaths()
    if 1 != len(excluded) || "/download" != excluded[0] {
        t.Fatalf("expected the configuration to keep its own copy, got: %v", excluded)
    }

    excluded[0] = "/other"

    if "/download" != config.ExcludedPaths()[0] {
        t.Fatalf("expected the accessor to hand out a copy as well")
    }

    config.SetExcludedPaths(nil)

    if nil != config.ExcludedPaths() {
        t.Fatalf("expected an explicit nil to clear the list, got: %v", config.ExcludedPaths())
    }
}

/* the shipped defaults are a policy, not an implementation detail: they decide what a deployment that configures nothing compresses. The media types are excluded because they are already compressed, and re-compressing them spends processor time to make the body larger. */

func TestDefaultCompressionConfig_ExcludesTheAlreadyCompressedMediaTypes(t *testing.T) {
    config := DefaultCompressionConfig()

    if 1024 != config.MinSize() {
        t.Fatalf("unexpected default minimum size: %d", config.MinSize())
    }

    excluded := config.ExcludedContentTypes()

    for _, required := range []string{"image/", "video/", "audio/"} {
        found := false

        for _, candidate := range excluded {
            if required == candidate {
                found = true

                break
            }
        }

        if false == found {
            t.Fatalf("expected %q to be excluded by default, got: %v", required, excluded)
        }
    }
}

/* DefaultCompressionMiddleware is the one-call front door and had no test. It has to be the default configuration wired through — a middleware that compressed nothing, or one that ignored the shipped minimum size, would read as working on every response large enough to compress. */

func TestDefaultCompressionMiddleware_CompressesAboveTheDefaultMinimumSize(t *testing.T) {
    middleware := DefaultCompressionMiddleware()

    body := strings.Repeat("melody ", 500)

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        response := http.NewResponse(nethttp.StatusOK, []byte(body))
        response.Headers().Set("Content-Type", "text/plain")

        return response, nil
    }

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    response, err := middleware(next)(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "gzip" != response.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the default middleware to compress, got encoding: %q", response.Headers().Get("Content-Encoding"))
    }
}

func TestDefaultCompressionMiddleware_SkipsBelowTheDefaultMinimumSize(t *testing.T) {
    middleware := DefaultCompressionMiddleware()

    next := func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        response := http.NewResponse(nethttp.StatusOK, []byte("small"))
        response.Headers().Set("Content-Type", "text/plain")

        return response, nil
    }

    request := httptest.NewRequest(nethttp.MethodGet, "/x", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    response, err := middleware(next)(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "" != response.Headers().Get("Content-Encoding") {
        t.Fatalf("expected a body under the default minimum size to be left alone")
    }
}

func TestCompressionMiddleware_JoinsEveryLineOfARepeatedAcceptEncodingField(t *testing.T) {
    config := NewCompressionConfig(6, 16, nil, nil)
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
            response.SetBodyReader(strings.NewReader(strings.Repeat("a", 64)))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Add("Accept-Encoding", "br")
    request.Header.Add("Accept-Encoding", "gzip")

    resultResponse, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the gzip named on the second field line to be honoured, got encoding %q", resultResponse.Headers().Get("Content-Encoding"))
    }
}

func TestAcceptsGzip_ARepeatedCodingResolvesToItsHigherQuality(t *testing.T) {
    if false == acceptsGzip("gzip;q=0.5, gzip;q=0") {
        t.Fatalf("expected the higher q to win regardless of order")
    }

    if false == acceptsGzip("gzip;q=0, gzip;q=0.5") {
        t.Fatalf("expected the reversed spelling to answer the same")
    }

    if true == acceptsGzip("gzip;q=0, gzip;q=0") {
        t.Fatalf("expected a repeated refusal to stay a refusal")
    }
}

func TestAcceptsGzip_QuotedSeparatorsStayInsideTheirParameter(t *testing.T) {
    if false == acceptsGzip(`gzip;note="a,b";q=0.5`) {
        t.Fatalf("expected the quoted comma to stay inside the parameter")
    }
}

func TestCompressionMiddleware_AnOversizedMinSizeDoesNotAllocateItUpfront(t *testing.T) {
    config := NewCompressionConfig(6, 1<<30, nil, nil)
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
            response.SetBodyReader(strings.NewReader("small"))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    var before runtime.MemStats
    runtime.ReadMemStats(&before)

    resultResponse, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    body, readErr := io.ReadAll(resultResponse.BodyReader())
    if nil != readErr || "small" != string(body) {
        t.Fatalf("expected the under-threshold body unchanged, got %q err=%v", body, readErr)
    }

    var after runtime.MemStats
    runtime.ReadMemStats(&after)

    /* TotalAlloc is monotonic and precise: the upfront allocation this guard forbids was the full GiB threshold, three orders of magnitude above this bound */
    if allocatedDelta := after.TotalAlloc - before.TotalAlloc; 100*1024*1024 < allocatedDelta {
        t.Fatalf("expected the oversized threshold not to be allocated upfront, allocated %d bytes", allocatedDelta)
    }
}

func TestCompressionMiddleware_TheGrowingPeekStillCompressesABodyAtTheThreshold(t *testing.T) {
    config := NewCompressionConfig(6, 100000, nil, nil)
    middleware := CompressionMiddleware(config)

    payload := strings.Repeat("a", 100000)

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
            response.SetBodyReader(iotest.OneByteReader(strings.NewReader(payload)))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
    request.Header.Set("Accept-Encoding", "gzip")

    resultResponse, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the threshold body to be compressed, got encoding %q", resultResponse.Headers().Get("Content-Encoding"))
    }

    gzipReader, gzipErr := gzip.NewReader(resultResponse.BodyReader())
    if nil != gzipErr {
        t.Fatalf("gzip reader: %v", gzipErr)
    }

    decompressed, readErr := io.ReadAll(gzipReader)
    if nil != readErr || payload != string(decompressed) {
        t.Fatalf("expected the grown peek to preserve every byte, len=%d err=%v", len(decompressed), readErr)
    }
}

/* nil reads as the default configuration, the way the cors middleware and the route group read their absent options: the nil dereference answered a wiring shorthand with a raw panic. */
func TestCompressionMiddleware_NilConfigReadsAsTheDefaultConfiguration(t *testing.T) {
    resultResponse := func() httpcontract.Response {
        middleware := CompressionMiddleware(nil)

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
                response.SetBodyReader(bytes.NewReader([]byte(strings.Repeat("hello world ", 200))))

                return response, nil
            },
        )

        request := httptest.NewRequest(nethttp.MethodGet, "/test", nil)
        request.Header.Set("Accept-Encoding", "gzip")

        resultResponse, err := handler(nil, httptest.NewRecorder(), testhelper.NewHttpTestRequestFromHttpRequest(request))
        if nil != err {
            t.Fatalf("expected nil error, got: %v", err)
        }

        return resultResponse
    }()

    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the default configuration to serve gzip, got %q", resultResponse.Headers().Get("Content-Encoding"))
    }
}

/* compressThroughStream runs one response through the streaming compressor the middleware uses and returns what a client would have decompressed */
func compressThroughStream(t *testing.T, payload string, level int) string {
    t.Helper()

    pipeReader, pipeWriter := io.Pipe()
    compressionDone := make(chan struct{})

    source := strings.NewReader(payload)

    go streamGzipCompressInto(pipeWriter, source, source, level, compressionDone)

    gzipReader, gzipErr := gzip.NewReader(pipeReader)
    if nil != gzipErr {
        t.Fatalf("expected a readable gzip stream, got: %v", gzipErr)
    }

    decompressed, readErr := io.ReadAll(gzipReader)
    if nil != readErr {
        t.Fatalf("expected the whole body back, got: %v", readErr)
    }

    _ = gzipReader.Close()
    _ = pipeReader.Close()

    <-compressionDone

    return string(decompressed)
}

/* the writers are pooled, so the second response is served by the writer the first one finished with: the reset onto the new pipe is the whole of what separates them, and without it the second body is written into a pipe nobody is reading any more */
func TestCompressionMiddleware_APooledGzipWriterServesTheNextResponseFromItsOwnBody(t *testing.T) {
    first := strings.Repeat("first-response-payload ", 64)
    second := strings.Repeat("second-response-payload ", 64)

    if decompressed := compressThroughStream(t, first, gzip.DefaultCompression); first != decompressed {
        t.Fatalf("expected the first response to decompress to its own body")
    }

    if decompressed := compressThroughStream(t, second, gzip.DefaultCompression); second != decompressed {
        t.Fatalf("expected the second response to decompress to its own body, not the first one's writer state")
    }
}

/* a writer goes back to the pool only once Close has returned: a writer returned while its response is still being written would be handed to another response in flight and the two bodies would interleave into one stream. Sixteen at once is what makes that observable, and each body is distinct so a crossed pair is named rather than merely detected. */
func TestCompressionMiddleware_ConcurrentResponsesDoNotShareAPooledWriter(t *testing.T) {
    var waitGroup sync.WaitGroup

    for index := 0; index < 16; index++ {
        waitGroup.Add(1)

        go func(index int) {
            defer waitGroup.Done()

            payload := strings.Repeat("payload-"+strconv.Itoa(index)+" ", 128)

            if decompressed := compressThroughStream(t, payload, gzip.DefaultCompression); payload != decompressed {
                t.Errorf("response %d decompressed to another response's body", index)
            }
        }(index)
    }

    waitGroup.Wait()
}

/* a level gzip refuses has no pool and must still be reported where it is made, rather than silently falling back to a default the caller never asked for */
func TestCompressionMiddleware_AnInvalidCompressionLevelIsRefused(t *testing.T) {
    if _, acquireErr := acquireGzipWriter(io.Discard, highestGzipLevel+1); nil == acquireErr {
        t.Fatalf("expected a level above BestCompression to be refused")
    }

    if _, acquireErr := acquireGzipWriter(io.Discard, lowestGzipLevel-1); nil == acquireErr {
        t.Fatalf("expected a level below HuffmanOnly to be refused")
    }
}

/* a gzip body is shorter than the plain one the handler measured, so a Content-Length that survived the compression names bytes that are never sent: a client reading that many either truncates the frame or waits for a remainder that never arrives, and a shared cache stores the mismatch. The header describes what is actually on the wire, so compressing has to drop it. */
func TestCompressionMiddleware_DropsTheContentLengthTheUncompressedBodyDeclared(t *testing.T) {
    config := NewCompressionConfig(6, 10, nil, nil)
    middleware := CompressionMiddleware(config)

    body := strings.Repeat("melody ", 200)
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
            responseHeaders.Set("Content-Length", strconv.Itoa(len(body)))
            response.SetHeaders(responseHeaders)
            response.SetBodyReader(bytes.NewReader([]byte(body)))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "gzip" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf("expected the body to be compressed, got Content-Encoding %q", resultResponse.Headers().Get("Content-Encoding"))
    }

    if "" != resultResponse.Headers().Get("Content-Length") {
        t.Fatalf(
            "a compressed body must not carry the plain body's length, got Content-Length %q over %d plain bytes",
            resultResponse.Headers().Get("Content-Length"),
            len(body),
        )
    }

    gzipReader, gzipErr := gzip.NewReader(resultResponse.BodyReader())
    if nil != gzipErr {
        t.Fatalf("gzip reader: %v", gzipErr)
    }

    decompressed, readErr := io.ReadAll(gzipReader)
    if nil != readErr {
        t.Fatalf("read decompressed: %v", readErr)
    }

    if body != string(decompressed) {
        t.Fatalf("expected the original body to survive compression, got %d bytes", len(decompressed))
    }
}

/* the short circuit takes the length the handler declared and passes the response through without buffering a byte of it, which is the whole reason it sits above the peek loop. For a body that really is that small the peek loop reaches the same verdict, so the two are indistinguishable there; the input that separates them is a header that under-declares, where the short circuit answers on the handler's word and the peek loop reads the body and compresses it. */
func TestCompressionMiddleware_ADeclaredLengthBelowTheMinimumIsTakenWithoutReadingTheBody(t *testing.T) {
    config := NewCompressionConfig(6, 4096, nil, nil)
    middleware := CompressionMiddleware(config)

    body := strings.Repeat("melody ", 2000)
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
            responseHeaders.Set("Content-Length", "12")
            response.SetHeaders(responseHeaders)
            response.SetBodyReader(bytes.NewReader([]byte(body)))

            return response, nil
        },
    )

    request := httptest.NewRequest(nethttp.MethodGet, "/", nil)
    request.Header.Set("Accept-Encoding", "gzip")
    melodyRequest := testhelper.NewHttpTestRequestFromHttpRequest(request)

    resultResponse, err := handler(nil, httptest.NewRecorder(), melodyRequest)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "" != resultResponse.Headers().Get("Content-Encoding") {
        t.Fatalf(
            "expected the declared length to be taken at its word and the body left alone, got Content-Encoding %q",
            resultResponse.Headers().Get("Content-Encoding"),
        )
    }

    if "12" != resultResponse.Headers().Get("Content-Length") {
        t.Fatalf("expected the declared length to survive untouched, got %q", resultResponse.Headers().Get("Content-Length"))
    }

    passedThrough, readErr := io.ReadAll(resultResponse.BodyReader())
    if nil != readErr {
        t.Fatalf("read passed-through body: %v", readErr)
    }

    if body != string(passedThrough) {
        t.Fatalf("expected the body to pass through verbatim, got %d of %d bytes", len(passedThrough), len(body))
    }
}

/* a Vary field is a comma-separated list, so the token this middleware needs can already be sitting beside another one that some other layer added. Comparing the whole field against the single token misses it and appends a second entry naming Accept-Encoding twice — a shared cache reads the repeated key as a different set of dimensions than the one the response was stored under. */
func TestAddVaryAcceptEncoding_ReadsAnExistingFieldTokenByToken(t *testing.T) {
    headers := nethttp.Header{}
    headers.Set("Vary", "Origin, Accept-Encoding")

    addVaryAcceptEncoding(headers)

    if 1 != len(headers.Values("Vary")) {
        t.Fatalf("expected the token already in the list to be found, got: %v", headers.Values("Vary"))
    }

    if "Origin, Accept-Encoding" != headers.Get("Vary") {
        t.Fatalf("expected the existing list to be left as it was, got: %q", headers.Get("Vary"))
    }
}

/* header field names are case-insensitive, so a layer that wrote the token in another case named the same dimension. The lowercase spelling is the one the comparison already carries, so it separates nothing; an upper-case one is recognised only if the case is folded, and nothing else in this package writes it. */
func TestAddVaryAcceptEncoding_ReadsAnExistingTokenInAnyCase(t *testing.T) {
    headers := nethttp.Header{}
    headers.Set("Vary", "ACCEPT-ENCODING")

    addVaryAcceptEncoding(headers)

    if 1 != len(headers.Values("Vary")) {
        t.Fatalf("expected the upper-case spelling to be recognised, got: %v", headers.Values("Vary"))
    }

    if "ACCEPT-ENCODING" != headers.Get("Vary") {
        t.Fatalf("expected the existing spelling to be left as it was, got: %q", headers.Get("Vary"))
    }
}

type panickingResponseBodyReader struct {
    servedOnce bool
}

func (instance *panickingResponseBodyReader) Read(destination []byte) (int, error) {
    if false == instance.servedOnce {
        instance.servedOnce = true

        served := len(destination)
        if 2048 < served {
            served = 2048
        }

        for index := 0; index < served; index++ {
            destination[index] = 'a'
        }

        return served, nil
    }

    panic("the application's body reader panicked")
}

/* the copy runs on a goroutine this middleware starts, where neither of the kernel's recovery defers nor net/http's own per-connection recovery stands: a panic raised by the application's body reader took the whole process down, every in-flight request with it. It is contained and reported through the pipe the reader of this response already fails on. */
func TestStreamGzipCompressInto_ContainsAPanicFromTheResponseBodyReader(t *testing.T) {
    pipeReader, pipeWriter := io.Pipe()
    compressionDone := make(chan struct{})

    go streamGzipCompressInto(pipeWriter, &panickingResponseBodyReader{}, nethttp.NoBody, 5, compressionDone)

    _, readErr := io.Copy(io.Discard, pipeReader)
    if nil == readErr {
        t.Fatal("expected the reader of the compressed body to see the failure")
    }

    if false == strings.Contains(readErr.Error(), "panicked while the response was being compressed") {
        t.Fatalf("expected the contained panic to be reported through the pipe, got %v", readErr)
    }

    select {
    case <-compressionDone:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the compression goroutine to finish")
    }
}
