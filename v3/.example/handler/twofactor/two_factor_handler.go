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

/* EnrollHandler enrolls the caller's TOTP second factor: it generates a secret and single-use recovery codes, persists them encrypted, and answers the secret, the otpauth URI and the recovery codes once, to the account they belong to. Enrolling again replaces the secret and its unused recovery codes, so an account whose authenticator is lost has a way back. */
func EnrollHandler(storeSource store2fa.StoreSource) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        store, storeErr := storeSource(runtimeInstance)
        if nil != storeErr {
            return unavailableStore(runtimeInstance, request, storeErr), nil
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

/* VerifyHandler verifies a second factor for the caller's own enrollment: a TOTP code on X-2FA-Code against the stored secret, or a single-use recovery code on X-2FA-Recovery-Code redeemed atomically; 200 on success, 401 on a wrong or replayed factor. An accepted code is burned in a replay guard keyed on the normalized code, since Verify normalizes before comparing. */
func VerifyHandler(storeSource store2fa.StoreSource) melodyhttpcontract.Handler {
    replayGuard := melodysecurity.NewMemoryNonceGuard()

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        store, storeErr := storeSource(runtimeInstance)
        if nil != storeErr {
            return unavailableStore(runtimeInstance, request, storeErr), nil
        }

        if recoveryCode := request.Header(melodysecurity.DefaultTotpRecoveryHeaderName); "" != recoveryCode {
            redeemed, redeemErr := store.RedeemRecoveryCode(runtimeInstance, user, recoveryCode)
            if nil != redeemErr {
                return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not redeem the recovery code", redeemErr), nil
            }

            if false == redeemed {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid recovery code"), nil
            }

            return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"factor": "recovery", "redeemed": true}), nil
        }

        code := request.Header(melodysecurity.DefaultTotpCodeHeaderName)
        if "" == code {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "a TOTP code or recovery code header is required"), nil
        }

        secret, enrolled, findErr := store.FindTotpSecret(runtimeInstance, user)
        if nil != findErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not look up the enrollment", findErr), nil
        }

        if false == enrolled {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "user is not enrolled"), nil
        }

        verified, verifyErr := totp.Verify(secret, code, totp.Config{})
        if nil != verifyErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", verifyErr), nil
        }

        if false == verified {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid code"), nil
        }

        seen, rememberErr := replayGuard.Remember(runtimeInstance, "2fa:"+user+":"+totp.NormalizeCode(code), totpCodeValidityWindow())
        if nil != rememberErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", rememberErr), nil
        }

        if true == seen {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "code already used"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"factor": "totp", "verified": true}), nil
    }
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
