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

/* enrolledIdentifier answers the account both doors act on: the identifier of the authenticated token, never a name the request chose.

   The two doors used to read a `user` query parameter, and the route was public. That pair let anyone bind a second factor they held to any identifier they liked and read back whether a code satisfied it, and — the insert being a plain one — left the named account unable to enroll ever after. Taken from the token, the enrollment door writes the caller's own row and the verification door reads it, so replacing an enrollment is the account's own doing. */
func enrolledIdentifier(runtimeInstance melodyruntimecontract.Runtime) (string, bool) {
    securityContext, exists := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        return "", false
    }

    identifier := securityContext.Token().UserIdentifier()
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

/* EnrollHandler enrolls the CALLER's TOTP second factor: it generates a secret and single-use recovery codes, persists them encrypted, and returns the secret + otpauth URI (the QR payload) and the recovery codes to show once. The secret is returned only here, and only to the account it belongs to — which is what the authenticated route buys. Enrolling again replaces the previous secret and its unused recovery codes, so an account whose authenticator is lost has a way back. */
func EnrollHandler(store *store2fa.Store) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
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

/* VerifyHandler verifies a second factor submitted for the CALLER's own enrollment: a TOTP code on the X-2FA-Code header is checked against the stored secret, or a single-use recovery code on X-2FA-Recovery-Code is atomically redeemed. It reports 200 on success and 401 on a wrong/replayed factor, over the same store the framework's TOTP authenticator would read if this example registered one — it does not; the two routes are how the store is exercised here.

   An accepted TOTP code stays valid for its whole window, so — exactly as the framework's authenticator does — it is burned in a replay guard the moment it is accepted. The nonce is keyed on the NORMALIZED code, because Verify normalizes before comparing: keying on the raw code would let "409 643" replay a code already spent as "409643". */
func VerifyHandler(store *store2fa.Store) melodyhttpcontract.Handler {
    replayGuard := melodysecurity.NewMemoryNonceGuard()

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
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

/* totpCodeValidityWindow is the span an accepted code stays verifiable — (2*skew+1) periods — and therefore how long a spent code must stay burned. It resolves through the totp package so it can never drift from what Verify honours. */
func totpCodeValidityWindow() time.Duration {
    resolved := totp.Config{}.Resolve()

    return time.Duration(2*resolved.Skew+1) * time.Duration(resolved.Period) * time.Second
}
