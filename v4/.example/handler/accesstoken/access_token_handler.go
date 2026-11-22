package accesstoken

import (
    "crypto/rand"
    "encoding/base64"
    nethttp "net/http"
    "time"

    examplejournal "github.com/precision-soft/melody/v4/.example/journal"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/repository"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    "github.com/precision-soft/melody/v4/.example/service"
    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
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

func IssueHandler(deviceIdentifierMaximumBytes int) melodyhttpcontract.Handler {
    return melodyhttp.JsonHandler(func(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request, body issueRequest) (melodyhttpcontract.Response, error) {
        principal, exists := examplesecurity.TokenFromRuntime(runtimeInstance)
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        if false == validDeviceIdentifier(body.DeviceIdentifier, deviceIdentifierMaximumBytes) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, deviceIdentifierRefusal(deviceIdentifierMaximumBytes)), nil
        }
        store := examplesecurity.TokenStoreFromRuntime(runtimeInstance)

        tokenString, tokenErr := newOpaqueToken()
        if nil != tokenErr {
            return nil, tokenErr
        }

        if putErr := store.Put(
            tokenString,
            melodysecuritycontract.Claims{
                UserIdentifier:   principal.UserIdentifier(),
                Roles:            principal.Roles(),
                DeviceIdentifier: body.DeviceIdentifier,
            },
            bound.Of(accessTokenLifetime),
        ); nil != putErr {
            return nil, putErr
        }

        /* a deletion between the session's resolution and the write released the tokens before this one existed */
        account, accountFound, accountErr := repository.MustGetUserRepository(runtimeInstance.Context(), runtimeInstance.Scope()).FindById(runtimeInstance.Context(), principal.UserIdentifier())
        if nil != accountErr {
            withdrawMintedToken(store, tokenString)

            return nil, accountErr
        }

        if false == accountFound || nil == account {
            withdrawMintedToken(store, tokenString)

            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        stored, found, lookupErr := store.Lookup(tokenString)
        if nil != lookupErr {
            withdrawMintedToken(store, tokenString)

            return nil, lookupErr
        }

        if false == found {
            withdrawMintedToken(store, tokenString)

            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "the minted token did not resolve"), nil
        }

        return melodyhttp.JsonResponse(nethttp.StatusCreated, issuedToken{
            Token:            tokenString,
            DeviceIdentifier: stored.DeviceIdentifier,
            IssuedAt:         stored.IssuedAt,
        })
    }, melodyhttp.JsonHandlerConfig{})
}

/* the revoke-device door holds the identifier to the ceiling rather than the configured bound, so a token issued before the bound was lowered stays revocable per device */
func RevokeDeviceHandler() melodyhttpcontract.Handler {
    return melodyhttp.JsonHandler(func(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request, body revokeDeviceRequest) (melodyhttpcontract.Response, error) {
        token, exists := examplesecurity.TokenFromRuntime(runtimeInstance)
        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        if false == validDeviceIdentifier(body.DeviceIdentifier, DeviceIdentifierMaximumBytesCeiling) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, deviceIdentifierRefusal(DeviceIdentifierMaximumBytesCeiling)), nil
        }

        userIdentifier := token.UserIdentifier()

        return publishBoundary(runtimeInstance, userIdentifier, body.DeviceIdentifier)
    }, melodyhttp.JsonHandlerConfig{})
}

func RevokeUserHandler() melodyhttpcontract.Handler {
    return melodyhttp.JsonHandler(func(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request, body revokeUserRequest) (melodyhttpcontract.Response, error) {
        if "" == body.UserIdentifier {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "userIdentifier is required"), nil
        }

        if false == service.CacheSafeIdentifier(body.UserIdentifier) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "userIdentifier names no account"), nil
        }

        return publishBoundary(runtimeInstance, body.UserIdentifier, "")
    }, melodyhttp.JsonHandlerConfig{})
}

func publishBoundary(
    runtimeInstance melodycontainercontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (melodyhttpcontract.Response, error) {
    /* the boundary is published over a runtime detached from the request's cancellation, as the withdraw and the release are: a client that hangs up cannot leave the revocation it asked for undone, and the store's CallTimeout still bounds the round trip */
    store := examplesecurity.TokenStoreFromRuntime(examplesecurity.DetachedRuntime(runtimeInstance))
    instant := melodyclock.ClockMustFromRuntime(runtimeInstance).Now()

    if revokeErr := store.RevokeBefore(userIdentifier, deviceIdentifier, instant); nil != revokeErr {
        return nil, revokeErr
    }

    published, epochErr := store.RevocationEpoch(userIdentifier, deviceIdentifier)
    if nil != epochErr {
        return nil, epochErr
    }

    return melodyhttp.JsonResponse(nethttp.StatusOK, revocation{
        UserIdentifier:   userIdentifier,
        DeviceIdentifier: deviceIdentifier,
        RevokedBefore:    published,
    })
}

/* the withdraw binds the store again over a runtime detached from the request's cancellation, so a client that hung up cannot leave the minted token live */
func withdrawMintedToken(
    store melodysecurity.BoundTokenStore,
    tokenString string,
) {
    runtimeInstance := store.Runtime()
    if deleteErr := melodysecurity.TokenStoreFor(examplesecurity.DetachedRuntime(runtimeInstance), store.TokenStore()).Delete(tokenString); nil != deleteErr {
        examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Error(
            "a minted access token could not be withdrawn",
            map[string]any{"error": deleteErr.Error()},
        )
    }
}

func newOpaqueToken() (string, error) {
    material := make([]byte, 32)
    if _, readErr := rand.Read(material); nil != readErr {
        return "", readErr
    }

    return base64.RawURLEncoding.EncodeToString(material), nil
}
