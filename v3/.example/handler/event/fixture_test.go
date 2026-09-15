package event

import (
    "context"
    "net/http/httptest"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    nethttp "net/http"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    "testing"
    "time"
)

type recordingResponseWriter struct {
    header      nethttp.Header
    wroteHeader bool
    status      int
    flushed     bool
}

func (instance *recordingResponseWriter) Header() nethttp.Header {
    if nil == instance.header {
        instance.header = nethttp.Header{}
    }

    return instance.header
}

func (instance *recordingResponseWriter) Write(payload []byte) (int, error) {
    if false == instance.wroteHeader {
        instance.WriteHeader(nethttp.StatusOK)
    }

    return len(payload), nil
}

func (instance *recordingResponseWriter) WriteHeader(statusCode int) {
    if true == instance.wroteHeader {
        return
    }

    instance.wroteHeader = true
    instance.status = statusCode
}

func (instance *recordingResponseWriter) Flush() {
    instance.flushed = true
}

func (instance *recordingResponseWriter) HeadersWritten() bool {
    return instance.wroteHeader
}

func (instance *recordingResponseWriter) CommittedStatusCode() int {
    return instance.status
}

func streamRequest(t *testing.T, target string, cancelled bool) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    return streamRequestOnContainer(t, target, cancelled, true)
}

func streamRequestWithoutHub(t *testing.T, target string) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    return streamRequestOnContainer(t, target, false, false)
}

func streamRequestOnContainer(t *testing.T, target string, cancelled bool, withHub bool) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()

    if true == withHub {
        hub := melodyhttp.NewServerSentEventHub()

        melodycontainer.MustRegister(
            containerInstance,
            subscriber.ServiceCatalogNotificationHub,
            func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
                return hub, nil
            },
        )
    }

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, target, nil)

    if true == cancelled {
        requestContext, cancel := context.WithCancel(context.Background())
        cancel()

        httpRequest = httpRequest.WithContext(requestContext)
    }

    request := melodyhttp.NewRequest(
        httpRequest,
        nil,
        runtimeInstance,
        melodyhttp.NewRequestContext("stream-test", time.Now()),
    )

    return request, runtimeInstance
}

type failingStreamWriter struct { recordingResponseWriter; failure error }

func (instance *failingStreamWriter) Write(payload []byte) (int, error) { return 0, instance.failure }
