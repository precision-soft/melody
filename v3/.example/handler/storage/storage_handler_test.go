package storage

import (
    "bytes"
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
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
