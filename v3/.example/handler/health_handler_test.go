package handler

import (
    "context"
    "encoding/json"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

/* the liveness probe reports the version the build stamped, the one main hands Configure, beside its status */
func TestHealthHandler_ReportsTheVersionItWasBuiltWith(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(containerInstance, melodyclock.ServiceClock, func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    })
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/health", nil), nil, runtimeInstance, melodyhttp.NewRequestContext("health-test", time.Now()))

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
