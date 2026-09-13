package secure

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
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
        securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        token := securityContext.Token()

        payload := mePayload{
            UserIdentifier: token.UserIdentifier(),
            Roles:          token.Roles(),
        }

        /* when the request is running under switch-user impersonation, surface the accountable admin behind the impersonated principal so the response shows both identities. */
        if impersonator, present := melodysecurity.ImpersonatorFromToken(token); true == present {
            payload.Impersonator = &principalView{
                UserIdentifier: impersonator.UserIdentifier(),
                Roles:          impersonator.Roles(),
            }
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}
