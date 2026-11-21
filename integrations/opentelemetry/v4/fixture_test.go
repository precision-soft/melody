package opentelemetry

import (
    "bufio"
    "context"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func testRequestAndRuntime() (httpcontract.Request, containercontract.Runtime) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/orders/42", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, nil)

    return request, runtimeInstance
}

func okHandler() httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{"ok": true})
    }
}

/* streamingTestWriter is a writer that can take a write deadline and be hijacked, so a test can require the recording writer to leave both reachable to the handler behind it */
type streamingTestWriter struct {
    *httptest.ResponseRecorder
    deadlineSet bool
    hijacked    bool
}

func (instance *streamingTestWriter) SetWriteDeadline(deadline time.Time) error {
    instance.deadlineSet = true

    return nil
}

func (instance *streamingTestWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    instance.hijacked = true

    return nil, nil, nil
}
