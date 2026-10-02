package twofactor

import (
    "time"

    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    store2fa "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    examplesecurity "github.com/precision-soft/melody/v3/.example/security"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/security/totp"
)

/* enrolledIdentifier answers the account both doors act on: the authenticated token's identifier, never a name the request chose, so the enrollment door writes the caller's own row and the verification door reads it. */
func enrolledIdentifier(runtimeInstance melodyruntimecontract.Runtime) (string, bool) {
    token, exists := examplesecurity.TokenFromRuntime(runtimeInstance)
    if false == exists {
        return "", false
    }

    identifier := token.UserIdentifier()
    if "" == identifier {
        return "", false
    }

    return identifier, true
}

type enrollPayload struct {
    UserIdentifier string   `json:"userIdentifier"`
    Secret         string   `json:"secret"`
    OtpauthUri     string   `json:"otpauthUri"`
    RecoveryCodes  []string `json:"recoveryCodes"`
}

/* EnrollHandler enrolls the caller's TOTP second factor: it generates a secret and single-use recovery codes, persists them encrypted, and answers the secret, the otpauth URI and the recovery codes once, to the account they belong to. A first enrollment needs the session alone. Enrolling again REPLACES the secret and its unused recovery codes, so it asks for a factor of the enrollment it replaces — a TOTP code on X-2FA-Code or a recovery code on X-2FA-Recovery-Code, checked and spent as the verification door spends them — and answers 401 with the challenge without one: a session alone, stolen or left open, must not become a second factor of the thief's. An account whose authenticator and codes are both lost is reset from the console (example:twofactor:reset). A nil guard is one of this door's own. */
func EnrollHandler(storeSource store2fa.StoreSource, replayGuard melodysecuritycontract.NonceGuard) melodyhttpcontract.Handler {
    if nil == replayGuard {
        replayGuard = melodysecurity.NewMemoryNonceGuard()
    }

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        store, storeErr := storeSource(runtimeInstance)
        if nil != storeErr {
            return unavailableStore(runtimeInstance, request, storeErr), nil
        }

        _, enrolled, findErr := store.FindTotpSecret(runtimeInstance, user)
        if nil != findErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not look up the enrollment", findErr), nil
        }

        if true == enrolled {
            _, refusal := currentFactor(runtimeInstance, request, store, user, replayGuard, func() melodyhttpcontract.Response {
                return presenter.ApiErrorWithPayload(runtimeInstance, request, nethttp.StatusUnauthorized, map[string]any{"factor": "totp"}, "second factor required")
            })
            if nil != refusal {
                return refusal, nil
            }
        }

        secret, uri, recoveryCodes, enrollErr := store.Enroll(runtimeInstance.Context(), user, "Melody Example")
        if nil != enrollErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not enroll the second factor", enrollErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, enrollPayload{
            UserIdentifier: user,
            Secret:         secret,
            OtpauthUri:     uri,
            RecoveryCodes:  recoveryCodes,
        }), nil
    }
}

/* VerifyHandler verifies a second factor for the caller's own enrollment: a TOTP code on X-2FA-Code against the stored secret, or a single-use recovery code on X-2FA-Recovery-Code redeemed atomically; 200 on success, 401 on a wrong or replayed factor. An accepted code is burned in the replay guard keyed on the normalized code, since Verify normalizes before comparing, under the nonce the framework's second-factor authenticator writes: handed the guard the sign-in reads, a code is spent once across both doors. A nil guard is one of this door's own. */
func VerifyHandler(storeSource store2fa.StoreSource, replayGuard melodysecuritycontract.NonceGuard) melodyhttpcontract.Handler {
    if nil == replayGuard {
        replayGuard = melodysecurity.NewMemoryNonceGuard()
    }

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        store, storeErr := storeSource(runtimeInstance)
        if nil != storeErr {
            return unavailableStore(runtimeInstance, request, storeErr), nil
        }

        factor, refusal := currentFactor(runtimeInstance, request, store, user, replayGuard, func() melodyhttpcontract.Response {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "a TOTP code or recovery code header is required")
        })
        if nil != refusal {
            return refusal, nil
        }

        if factorRecovery == factor {
            return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"factor": "recovery", "redeemed": true}), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"factor": "totp", "verified": true}), nil
    }
}

/* the two factors a request can present */
const (
    factorTotp     = "totp"
    factorRecovery = "recovery"
)

/* currentFactor checks the factor the request carries against the caller's enrollment and spends it: a recovery code on X-2FA-Recovery-Code is redeemed atomically, a TOTP code on X-2FA-Code is verified and burned in the replay guard. It answers the factor accepted, or the refusal to send — 401 for a wrong, spent or unenrolled factor, 500 for a store that failed, and what absent builds when the request carries neither header. */
func currentFactor(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    store *store2fa.Store,
    user string,
    replayGuard melodysecuritycontract.NonceGuard,
    absent func() melodyhttpcontract.Response,
) (string, melodyhttpcontract.Response) {
    if recoveryCode := request.Header(melodysecurity.DefaultTotpRecoveryHeaderName); "" != recoveryCode {
        redeemed, redeemErr := store.RedeemRecoveryCode(runtimeInstance, user, recoveryCode)
        if nil != redeemErr {
            return "", presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not redeem the recovery code", redeemErr)
        }

        if false == redeemed {
            return "", presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid recovery code")
        }

        return factorRecovery, nil
    }

    code := request.Header(melodysecurity.DefaultTotpCodeHeaderName)
    if "" == code {
        return "", absent()
    }

    secret, enrolled, findErr := store.FindTotpSecret(runtimeInstance, user)
    if nil != findErr {
        return "", presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not look up the enrollment", findErr)
    }

    if false == enrolled {
        return "", presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "user is not enrolled")
    }

    verified, verifyErr := totp.Verify(secret, code, totp.Config{})
    if nil != verifyErr {
        return "", presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", verifyErr)
    }

    if false == verified {
        return "", presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid code")
    }

    seen, rememberErr := replayGuard.Remember(runtimeInstance, "2fa:"+user+":"+totp.NormalizeCode(code), totpCodeValidityWindow())
    if nil != rememberErr {
        return "", presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", rememberErr)
    }

    if true == seen {
        return "", presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "code already used")
    }

    return factorTotp, nil
}

/* unavailableStore answers a door whose store could not be resolved with 503 and the cause journaled; the next request resolves the store again. */
func unavailableStore(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, storeErr error) melodyhttpcontract.Response {
    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the second factor is unavailable", storeErr)
}

/* totpCodeValidityWindow is how long an accepted code stays verifiable, (2*skew+1) periods, and so how long a spent code stays burned; it resolves through the totp package, so it matches what Verify honours. */
func totpCodeValidityWindow() time.Duration {
    resolved := totp.Config{}.Resolve()

    return time.Duration(2*resolved.Skew+1) * time.Duration(resolved.Period) * time.Second
}
