package handler

import (
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v4/.example/presenter"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type healthPayload struct {
    Status  string `json:"status"`
    Version string `json:"version"`
    Time    string `json:"time"`
}

/* HealthHandler answers the liveness probe with the version of the build that serves it, so a supervisor or a rollout reads which build answers without a shell on the host */
func HealthHandler(applicationVersion string) melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        payload := healthPayload{
            Status:  "ok",
            Version: applicationVersion,
            Time:    melodyclock.ClockMustFromRuntime(runtimeInstance).Now().Format(time.RFC3339),
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}
