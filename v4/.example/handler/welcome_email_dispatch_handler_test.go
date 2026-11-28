package handler

import (
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

func TestWelcomeEmailDispatchHandler_AnswersWithoutATransport503NamingTheKey(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/messagebus/dispatch", nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("dispatch-test", time.Now())})

    response, handlerErr := WelcomeEmailDispatchHandler(false)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nethttp.StatusServiceUnavailable != response.StatusCode() {
        t.Fatalf("expected 503 without a transport, got %v (%v)", response, handlerErr)
    }

    if body := bodyOf(t, response.BodyReader()); false == strings.Contains(body, "AMQP_DSN") {
        t.Fatalf("expected the refusal to name AMQP_DSN, got %s", body)
    }
}

func bodyOf(t *testing.T, reader io.Reader) string {
    t.Helper()

    if nil == reader {
        return ""
    }

    body, readErr := io.ReadAll(reader)
    if nil != readErr {
        t.Fatalf("read body: %v", readErr)
    }

    return string(body)
}
