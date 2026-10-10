package storage

import (
    "bytes"
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "net/url"
    "strings"
    "sync"
    "testing"
    "time"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

func TestLinkTtlOf_RefusesASecondsValueThatWrapsPastTheBound(t *testing.T) {
    for _, raw := range []string{"18446744074", "9223372036854775807"} {
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


/* objectServer answers the n-th HEAD with the n-th of headStatuses, the last one repeated, and a GET with getStatus, serving "stored" on a 200 GET: the door's existence check and the object's own stat are two HEADs */
func objectServer(t *testing.T, headStatuses []int, getStatus int) *melodyawss3.Storage {
    t.Helper()

    var mutex sync.Mutex
    heads := 0

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, httpRequest *nethttp.Request) {
        status := getStatus
        if nethttp.MethodHead == httpRequest.Method {
            mutex.Lock()
            status = headStatuses[min(heads, len(headStatuses)-1)]
            heads++
            mutex.Unlock()
        }

        writer.Header().Set("ETag", "\"9a0364b9e99bb480dd25e1f0284c8555\"")
        writer.Header().Set("Last-Modified", "Mon, 05 Oct 2026 10:00:00 GMT")
        writer.Header().Set("Content-Length", "6")
        writer.WriteHeader(status)
        if nethttp.MethodGet == httpRequest.Method && nethttp.StatusOK == status {
            _, _ = writer.Write([]byte("stored"))
        }
    }))
    t.Cleanup(server.Close)

    client, clientErr := melodyawss3.NewClient(melodyawss3.Config{Endpoint: strings.TrimPrefix(server.URL, "http://"), AccessKey: "test", SecretKey: "test", Region: "us-east-1"})
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }

    return melodyawss3.NewStorage(client, "melody-test")
}

/* errorRecordingLogger keeps the server-error records the presenter filed through the runtime's logger; the presenter's fallback records of a container without a serializer are not the door's */
type errorRecordingLogger struct {
    melodyloggingcontract.Logger
    mutex   sync.Mutex
    records []string
}

func (instance *errorRecordingLogger) Error(message string, context melodyloggingcontract.Context) {
    if "handler answered a server error" != message {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, message)
}

func (instance *errorRecordingLogger) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.records...)
}

func getObject(t *testing.T, storage *melodyawss3.Storage) (int, []string) {
    t.Helper()

    logger := &errorRecordingLogger{Logger: melodylogging.NewNopLogger()}
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, melodylogging.ServiceLogger, func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
        return logger, nil
    })
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/storage/object?key=probe.txt", nil), nil, runtimeInstance, melodyhttp.NewRequestContext("storage-get-test", time.Now()))

    response, handlerErr := GetHandler(storage)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("expected a response, got %v", handlerErr)
    }

    return response.StatusCode(), logger.recorded()
}

func TestGetHandler_AnswersAStoreRefusalWith500AndJournalsIt(t *testing.T) {
    status, records := getObject(t, objectServer(t, []int{nethttp.StatusForbidden}, nethttp.StatusOK))
    if nethttp.StatusInternalServerError != status || 1 != len(records) {
        t.Fatalf("expected 500 and one record for an existence check the store refused, got %d and %v", status, records)
    }

    status, records = getObject(t, objectServer(t, []int{nethttp.StatusOK, nethttp.StatusForbidden}, nethttp.StatusOK))
    if nethttp.StatusInternalServerError != status || 1 != len(records) {
        t.Fatalf("expected 500 and one record for a get the store refused after the existence check, got %d and %v", status, records)
    }

    status, records = getObject(t, objectServer(t, []int{nethttp.StatusOK, nethttp.StatusNotFound}, nethttp.StatusOK))
    if nethttp.StatusInternalServerError != status || 1 != len(records) {
        t.Fatalf("expected an object gone after the existence check answered 500, got %d and %v", status, records)
    }

    status, records = getObject(t, objectServer(t, []int{nethttp.StatusOK}, nethttp.StatusForbidden))
    if nethttp.StatusInternalServerError != status || 1 != len(records) {
        t.Fatalf("expected 500 and one record for a body the store refused to send, got %d and %v", status, records)
    }
}

func TestGetHandler_AnswersAMissingObject404(t *testing.T) {
    status, records := getObject(t, objectServer(t, []int{nethttp.StatusNotFound}, nethttp.StatusNotFound))
    if nethttp.StatusNotFound != status || 0 != len(records) {
        t.Fatalf("expected 404 and no record for an absent object, got %d and %v", status, records)
    }

    status, records = getObject(t, objectServer(t, []int{nethttp.StatusOK}, nethttp.StatusOK))
    if nethttp.StatusOK != status || 0 != len(records) {
        t.Fatalf("expected the stored object served, got %d and %v", status, records)
    }
}
