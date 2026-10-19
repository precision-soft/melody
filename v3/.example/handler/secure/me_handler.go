package secure

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    examplesecurity "github.com/precision-soft/melody/v3/.example/security"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

type mePayload struct {
    UserIdentifier string         `json:"userIdentifier"`
    Roles          []string       `json:"roles"`
    Impersonator   *principalView `json:"impersonator,omitempty"`
}

type principalView struct {
    UserIdentifier string   `json:"userIdentifier"`
    Roles          []string `json:"roles"`
}

func MeHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        token, exists := examplesecurity.TokenFromRuntime(runtimeInstance)
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        payload := mePayload{
            UserIdentifier: token.UserIdentifier(),
            Roles:          token.Roles(),
        }

        /* under switch-user impersonation the response shows the accountable admin beside the impersonated principal */
        if impersonator, present := melodysecurity.ImpersonatorFromToken(token); true == present {
            payload.Impersonator = &principalView{
                UserIdentifier: impersonator.UserIdentifier(),
                Roles:          impersonator.Roles(),
            }
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}
