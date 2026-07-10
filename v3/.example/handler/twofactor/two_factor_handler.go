package twofactor

import (
    "time"

    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    store2fa "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    "github.com/precision-soft/melody/v3/security/totp"
)

/* queryString reads a query parameter as a string, handling the bag's []string storage (query values are
kept as string slices) as well as a plain string, returning "" when absent. */
func queryString(request melodyhttpcontract.Request, name string) string {
    value, exists := request.Query().Get(name)
    if false == exists {
        return ""
    }

    switch typed := value.(type) {
    case string:
        return typed
    case []string:
        if 0 == len(typed) {
            return ""
        }

        return typed[0]
    default:
        return ""
    }
}

type enrollPayload struct {
    UserIdentifier string   `json:"userIdentifier"`
    Secret         string   `json:"secret"`
    OtpauthUri     string   `json:"otpauthUri"`
    RecoveryCodes  []string `json:"recoveryCodes"`
}

/* EnrollHandler enrolls a user's TOTP second factor: it generates a secret and single-use recovery codes,
persists them encrypted, and returns the secret + otpauth URI (the QR payload) and the recovery codes to
show once. A demo endpoint — a real app returns the secret only during enrollment and behind the user's
authenticated session. */
func EnrollHandler(store *store2fa.Store) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user := queryString(request, "user")
        if "" == user {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "user query parameter is required"), nil
        }

        secret, uri, recoveryCodes, enrollErr := store.Enroll(runtimeInstance.Context(), user, "Melody Example")
        if nil != enrollErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not enroll the second factor"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, enrollPayload{
            UserIdentifier: user,
            Secret:         secret,
            OtpauthUri:     uri,
            RecoveryCodes:  recoveryCodes,
        }), nil
    }
}

/* VerifyHandler verifies a submitted second factor for a user: a TOTP code on the X-2FA-Code header is
checked against the stored secret, or a single-use recovery code on X-2FA-Recovery-Code is atomically
redeemed. It reports 200 on success and 401 on a wrong/replayed factor, exercising the same store the
TotpSecondFactorAuthenticator uses.

An accepted TOTP code stays valid for its whole window, so — exactly as the framework's authenticator does — it is
burned in a replay guard the moment it is accepted. The nonce is keyed on the NORMALIZED code, because Verify
normalizes before comparing: keying on the raw code would let "409 643" replay a code already spent as "409643". */
func VerifyHandler(store *store2fa.Store) melodyhttpcontract.Handler {
    replayGuard := melodysecurity.NewMemoryNonceGuard()

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user := queryString(request, "user")
        if "" == user {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "user query parameter is required"), nil
        }

        if recoveryCode := request.Header(melodysecurity.DefaultTotpRecoveryHeaderName); "" != recoveryCode {
            redeemed, redeemErr := store.RedeemRecoveryCode(runtimeInstance, user, recoveryCode)
            if nil != redeemErr {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not redeem the recovery code"), nil
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
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not look up the enrollment"), nil
        }

        if false == enrolled {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "user is not enrolled"), nil
        }

        verified, verifyErr := totp.Verify(secret, code, totp.Config{})
        if nil != verifyErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code"), nil
        }

        if false == verified {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid code"), nil
        }

        seen, rememberErr := replayGuard.Remember(runtimeInstance, "2fa:"+user+":"+totp.NormalizeCode(code), totpCodeValidityWindow())
        if nil != rememberErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code"), nil
        }

        if true == seen {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "code already used"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"factor": "totp", "verified": true}), nil
    }
}

/* totpCodeValidityWindow is the span an accepted code stays verifiable — (2*skew+1) periods — and therefore how long a
spent code must stay burned. It resolves through the totp package so it can never drift from what Verify honours. */
func totpCodeValidityWindow() time.Duration {
    resolved := totp.Config{}.Resolve()

    return time.Duration(2*resolved.Skew+1) * time.Duration(resolved.Period) * time.Second
}
