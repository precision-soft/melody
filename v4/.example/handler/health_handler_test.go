package handler

import (
    "context"
    "encoding/json"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

func TestHealthHandler_ReportsTheVersionItWasBuiltWith(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister(containerInstance, melodyclock.ServiceClock, func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    }, melodycontainer.RegisterOptions{})
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/health", nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("health-test", time.Now())})

    response, handlerErr := HealthHandler("2026.10.08-test")(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected 200, got %v / %v", response, handlerErr)
    }

    bodyBytes, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read the body: %v", readErr)
    }

    document := struct {
        Payload struct {
            Status  string `json:"status"`
            Version string `json:"version"`
        } `json:"payload"`
    }{}
    if decodeErr := json.Unmarshal(bodyBytes, &document); nil != decodeErr {
        t.Fatalf("the body is not json (%v): %s", decodeErr, bodyBytes)
    }

    if "ok" != document.Payload.Status || "2026.10.08-test" != document.Payload.Version {
        t.Fatalf("expected status ok and the stamped version, got %s", bodyBytes)
    }
}
