package handler

import (
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

func TestWelcomeEmailDispatchHandler_AnswersWithoutATransport503NamingTheKey(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/messagebus/dispatch", nil), nil, runtimeInstance, melodyhttp.NewRequestContext("dispatch-test", time.Now()))

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
