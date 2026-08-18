package accesstoken

import (
    "crypto/rand"
    "encoding/base64"
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v3/.example/presenter"
    examplesecurity "github.com/precision-soft/melody/v3/.example/security"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

const accessTokenLifetime = time.Hour

type issueRequest struct {
    DeviceIdentifier string `json:"deviceIdentifier"`
}

type issuedToken struct {
    Token            string    `json:"token"`
    DeviceIdentifier string    `json:"deviceIdentifier"`
    IssuedAt         time.Time `json:"issuedAt"`
}

type revocation struct {
    UserIdentifier   string    `json:"userIdentifier"`
    DeviceIdentifier string    `json:"deviceIdentifier,omitempty"`
    RevokedBefore    time.Time `json:"revokedBefore"`
}

type revokeDeviceRequest struct {
    DeviceIdentifier string `json:"deviceIdentifier"`
}

type revokeUserRequest struct {
    UserIdentifier string `json:"userIdentifier"`
}

func IssueHandler() melodyhttpcontract.Handler {
    return melodyhttp.JsonHandler(func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, body issueRequest) (melodyhttpcontract.Response, error) {
        securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        if "" == body.DeviceIdentifier {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "deviceIdentifier is required"), nil
        }

        principal := securityContext.Token()
        store := examplesecurity.TokenStoreFromResolver(runtimeInstance.Container())

        tokenString, tokenErr := newOpaqueToken()
        if nil != tokenErr {
            return nil, tokenErr
        }

        store.PutWithTtl(
            tokenString,
            melodysecuritycontract.Claims{
                UserIdentifier:   principal.UserIdentifier(),
                Roles:            principal.Roles(),
                DeviceIdentifier: body.DeviceIdentifier,
            },
            accessTokenLifetime,
        )
        stored, found, lookupErr := store.Lookup(runtimeInstance, tokenString)
        if nil != lookupErr {
            return nil, lookupErr
        }

        if false == found {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "the minted token did not resolve"), nil
        }

        return melodyhttp.JsonResponse(nethttp.StatusCreated, issuedToken{
            Token:            tokenString,
            DeviceIdentifier: stored.DeviceIdentifier,
            IssuedAt:         stored.IssuedAt,
        })
    })
}

func RevokeDeviceHandler() melodyhttpcontract.Handler {
    return melodyhttp.JsonHandler(func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, body revokeDeviceRequest) (melodyhttpcontract.Response, error) {
        securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        if "" == body.DeviceIdentifier {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "deviceIdentifier is required"), nil
        }

        userIdentifier := securityContext.Token().UserIdentifier()

        return publishBoundary(runtimeInstance, userIdentifier, body.DeviceIdentifier)
    })
}

func RevokeUserHandler() melodyhttpcontract.Handler {
    return melodyhttp.JsonHandler(func(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, body revokeUserRequest) (melodyhttpcontract.Response, error) {
        if "" == body.UserIdentifier {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "userIdentifier is required"), nil
        }

        return publishBoundary(runtimeInstance, body.UserIdentifier, "")
    })
}

func publishBoundary(
    runtimeInstance melodyruntimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (melodyhttpcontract.Response, error) {
    store := examplesecurity.TokenStoreFromResolver(runtimeInstance.Container())
    instant := melodyclock.ClockMustFromResolver(runtimeInstance.Container()).Now()

    store.RevokeBefore(userIdentifier, deviceIdentifier, instant)

    published, epochErr := store.RevocationEpoch(runtimeInstance, userIdentifier, deviceIdentifier)
    if nil != epochErr {
        return nil, epochErr
    }

    return melodyhttp.JsonResponse(nethttp.StatusOK, revocation{
        UserIdentifier:   userIdentifier,
        DeviceIdentifier: deviceIdentifier,
        RevokedBefore:    published,
    })
}

func newOpaqueToken() (string, error) {
    material := make([]byte, 32)
    if _, readErr := rand.Read(material); nil != readErr {
        return "", readErr
    }

    return base64.RawURLEncoding.EncodeToString(material), nil
}
