package handler

import (
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/.example/presenter"
    melodyclock "github.com/precision-soft/melody/clock"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

type healthPayload struct {
    Status string `json:"status"`
    Time   string `json:"time"`
}

/* HealthHandler stamps its answer from the injected clock, like every other stamp the example writes. */
func HealthHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        payload := healthPayload{
            Status: "ok",
            Time:   melodyclock.ClockMustFromResolver(runtimeInstance.Container()).Now().Format(time.RFC3339),
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}
