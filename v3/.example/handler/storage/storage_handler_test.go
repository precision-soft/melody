package storage

import (
    "bytes"
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "net/url"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

type failingReader struct{}

func (instance failingReader) Read(payload []byte) (int, error) {
    return 0, errors.New("connection reset")
}

func putStatusFor(t *testing.T, body func(httpRequest *nethttp.Request)) int {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodPut, "/storage/object?key=probe", nil)
    body(httpRequest)

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("storage-test", time.Now()))

    response, handlerErr := PutHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("expected a response, got %v", handlerErr)
    }

    return response.StatusCode()
}

func TestPutHandler_AnswersABodyPastTheKernelLimitWith413(t *testing.T) {
    status := putStatusFor(t, func(httpRequest *nethttp.Request) {
        httpRequest.Body = nethttp.MaxBytesReader(httptest.NewRecorder(), readCloser{bytes.NewReader(make([]byte, 32))}, 8)
    })

    if nethttp.StatusRequestEntityTooLarge != status {
        t.Fatalf("expected 413 for a body past the limit, got %d", status)
    }
}

func TestPutHandler_AnswersAnUnreadableBodyWith400(t *testing.T) {
    status := putStatusFor(t, func(httpRequest *nethttp.Request) {
        httpRequest.Body = readCloser{failingReader{}}
    })

    if nethttp.StatusBadRequest != status {
        t.Fatalf("expected 400 for a body that cannot be read, got %d", status)
    }
}

type readCloser struct {
    reader interface {
        Read(payload []byte) (int, error)
    }
}

func (instance readCloser) Read(payload []byte) (int, error) {
    return instance.reader.Read(payload)
}

func (instance readCloser) Close() error {
    return nil
}

func linkRequest(t *testing.T, target string) *melodyhttp.Request {
    t.Helper()

    return melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, target, nil), nil, nil, melodyhttp.NewRequestContext("storage-link-test", time.Now()))
}

func TestLinkTtlOf_AnswersFiveMinutesWhenTheCallerAskedForNone(t *testing.T) {
    ttl, ttlErr := linkTtlOf(linkRequest(t, "/storage/object/link?key=probe"))
    if nil != ttlErr || 5*time.Minute != ttl {
        t.Fatalf("expected the five-minute default, got %s (%v)", ttl, ttlErr)
    }
}

func TestLinkTtlOf_ReadsTheCallersSecondsUpToAnHour(t *testing.T) {
    for raw, expected := range map[string]time.Duration{"1": time.Second, "90": 90 * time.Second, "3600": time.Hour} {
        ttl, ttlErr := linkTtlOf(linkRequest(t, "/storage/object/link?key=probe&ttl="+raw))
        if nil != ttlErr || expected != ttl {
            t.Fatalf("ttl=%s: expected %s, got %s (%v)", raw, expected, ttl, ttlErr)
        }
    }
}

func TestLinkTtlOf_RefusesATtlItCannotServe(t *testing.T) {
    for _, raw := range []string{"0", "-5", "3601", "5m", "x"} {
        if _, ttlErr := linkTtlOf(linkRequest(t, "/storage/object/link?key=probe&ttl="+raw)); false == errors.Is(ttlErr, errInvalidLinkTtl) {
            t.Fatalf("ttl=%s: expected the ttl refused, got %v", raw, ttlErr)
        }
    }
}

/* the object store refuses a key with a ".." segment by name, so every door answers it as the client's error, before the store is asked */
func TestHandlers_AnswerAKeyWithAParentSegmentWith400BeforeTheStore(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    for name, handler := range map[string]melodyhttpcontract.Handler{"put": PutHandler(nil), "get": GetHandler(nil), "link": LinkHandler(nil)} {
        for _, key := range []string{"tenant-b/../tenant-a/secret.txt", "..", "a\\..\\b"} {
            httpRequest := httptest.NewRequest(nethttp.MethodGet, "/storage/object?key="+url.QueryEscape(key), nil)
            request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("storage-test", time.Now()))

            response, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)
            if nil != handlerErr || nethttp.StatusBadRequest != response.StatusCode() {
                t.Fatalf("%s %q: expected 400, got %v (%v)", name, key, response, handlerErr)
            }
        }
    }
}

