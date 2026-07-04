package internalauth

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

type whoamiPayload struct {
    ServicePrincipal string      `json:"servicePrincipal"`
    Roles            []string    `json:"roles"`
    OnBehalfOf       *actorView  `json:"onBehalfOf,omitempty"`
}

type actorView struct {
    Identifier string            `json:"identifier"`
    Type       string            `json:"type"`
    Roles      []string          `json:"roles"`
    Attributes map[string]string `json:"attributes,omitempty"`
}

/* WhoamiHandler echoes the principal the internal-auth (HMAC) firewall authenticated: the calling service
and its registry roles, plus the originating actor the caller propagated (F1), if any. It reads the token
straight from the security context the firewall populated — a working demo that the HMAC envelope both
authenticates the service and carries the upstream human actor across the service boundary. */
func WhoamiHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        token := securityContext.Token()

        payload := whoamiPayload{
            ServicePrincipal: token.UserIdentifier(),
            Roles:            token.Roles(),
        }

        if actor, present := melodysecurity.ActorFromToken(token); true == present {
            payload.OnBehalfOf = &actorView{
                Identifier: actor.Identifier(),
                Type:       actor.Type(),
                Roles:      actor.Roles(),
                Attributes: actor.Attributes(),
            }
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, payload), nil
    }
}
