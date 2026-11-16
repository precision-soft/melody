package twofactor

import (
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "time"

    nethttp "net/http"

    "github.com/precision-soft/melody/v4/.example/page"
    "github.com/precision-soft/melody/v4/.example/presenter"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    store2fa "github.com/precision-soft/melody/v4/.example/twofactor"
    "github.com/precision-soft/melody/v4/bound"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
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

/* PageHandler serves the caller's second-factor page: the state, the enrollment with its QR code and recovery codes, and the confirmation. */
func PageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.TwoFactorHtml), nil
    }
}

type enrollPayload struct {
    UserIdentifier string   `json:"userIdentifier"`
    Secret         string   `json:"secret"`
    OtpauthUri     string   `json:"otpauthUri"`
    RecoveryCodes  []string `json:"recoveryCodes"`
    Pending        bool     `json:"pending"`
    PendingId      string   `json:"pendingId"`
}

/* PendingHeaderName names, on a confirmation, the set-up it confirms: the pendingId the enrollment answered */
const PendingHeaderName = "X-2FA-Pending"

/* pendingIdentifierOf names a set-up without echoing its secret: a confirmation carrying it is refused 409 once a later enroll replaced that set-up, rather than 401 against a secret its sender never saw */
func pendingIdentifierOf(secret string) string {
    sum := sha256.Sum256([]byte(secret))

    return hex.EncodeToString(sum[:8])
}

/* EnrollHandler starts the caller's TOTP second factor: it generates a secret and single-use recovery codes, persists them encrypted as a PENDING enrollment, and answers the secret, the otpauth URI and the recovery codes once, to the account they belong to. The pending enrollment is no factor until ConfirmHandler accepts a code computed from its secret; an active one keeps verifying until then. A first enrollment needs the session alone. Enrolling while a factor is active prepares its REPLACEMENT, so it asks for a factor of the enrollment it replaces — a TOTP code on X-2FA-Code or a recovery code on X-2FA-Recovery-Code, checked and spent as the verification door spends them — and answers 401 with the challenge without one: a session alone, stolen or left open, must not become a second factor of the thief's. An account whose authenticator and codes are both lost is reset from the console (example:twofactor:reset). A nil guard is one of this door's own. */
func EnrollHandler(storeSource store2fa.StoreSource, replayGuard melodysecuritycontract.NonceGuard, budget FactorBudget) melodyhttpcontract.Handler {
    if nil == replayGuard {
        replayGuard = melodysecurity.NewMemoryNonceGuard(melodysecurity.MemoryNonceGuardConfig{})
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
            _, refusal := currentFactor(runtimeInstance, request, store, user, replayGuard, budget, func() melodyhttpcontract.Response {
                return presenter.ApiErrorWithPayload(runtimeInstance, request, nethttp.StatusUnauthorized, map[string]any{"factor": "totp"}, "second factor required")
            })
            if nil != refusal {
                return refusal, nil
            }
        }

        secret, uri, recoveryCodes, enrollErr := store.Enroll(runtimeInstance.Context(), user, "Melody Example", enrolled)
        if nil != enrollErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not enroll the second factor", enrollErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, enrollPayload{
            UserIdentifier: user,
            Secret:         secret,
            OtpauthUri:     uri,
            RecoveryCodes:  recoveryCodes,
            Pending:        true,
            PendingId:      pendingIdentifierOf(secret),
        }), nil
    }
}

/* ConfirmHandler makes the caller's pending enrollment their factor: a TOTP code on X-2FA-Code computed from the pending secret proves the authenticator holds it, and the pending secret and recovery codes replace an active enrollment, only when the set-up presented that factor (see Store.ConfirmEnrollment). X-2FA-Pending, the pendingId the enrollment answered, names the set-up confirmed; a later one in its place answers 409. The code is burned in the replay guard the sign-in reads and spends the account's factor budget like the other two doors; 200 on success, 400 without a code, 401 on a wrong or replayed one, the pending enrollment kept, 409 when nothing is pending. A nil guard is one of this door's own. */
func ConfirmHandler(storeSource store2fa.StoreSource, replayGuard melodysecuritycontract.NonceGuard, budget FactorBudget) melodyhttpcontract.Handler {
    if nil == replayGuard {
        replayGuard = melodysecurity.NewMemoryNonceGuard(melodysecurity.MemoryNonceGuardConfig{})
    }

    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        code := request.Header(melodysecurity.DefaultTotpCodeHeaderName)
        if "" == code {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "a TOTP code header is required"), nil
        }

        store, storeErr := storeSource(runtimeInstance)
        if nil != storeErr {
            return unavailableStore(runtimeInstance, request, storeErr), nil
        }

        if refusal := refuseOverBudget(runtimeInstance, request, user, budget); nil != refusal {
            return refusal, nil
        }

        secret, pending, findErr := store.FindPendingSecret(runtimeInstance.Context(), user)
        if nil != findErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not look up the pending enrollment", findErr), nil
        }

        if false == pending {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "no enrollment is pending"), nil
        }

        if named := request.Header(PendingHeaderName); "" != named && named != pendingIdentifierOf(secret) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "the set-up was replaced by a later one"), nil
        }

        if refusal := acceptTotpCode(runtimeInstance, request, user, secret, code, replayGuard); nil != refusal {
            return refusal, nil
        }

        outcome, confirmErr := store.ConfirmEnrollment(runtimeInstance.Context(), user, secret)
        if nil != confirmErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not confirm the enrollment", confirmErr), nil
        }

        switch outcome {
        case store2fa.ConfirmNothingPending:
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "no enrollment is pending"), nil
        case store2fa.ConfirmReplaced:
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "the set-up was replaced by a later one"), nil
        case store2fa.ConfirmFactorBecameActive:
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusConflict, "a factor became active since the set-up started; replacing it asks for it"), nil
        }

        if nil != budget {
            budget.Release(user)
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"userIdentifier": user, "enrolled": true}), nil
    }
}

type statusPayload struct {
    Enrolled bool `json:"enrolled"`
    Pending  bool `json:"pending"`
}

/* StatusHandler answers whether the caller has an active factor and whether an enrollment waits for its confirmation, for the account page; it opens no sealed column. */
func StatusHandler(storeSource store2fa.StoreSource) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        user, authenticated := enrolledIdentifier(runtimeInstance)
        if false == authenticated {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "unauthorized"), nil
        }

        store, storeErr := storeSource(runtimeInstance)
        if nil != storeErr {
            return unavailableStore(runtimeInstance, request, storeErr), nil
        }

        enrolled, pending, stateErr := store.EnrollmentState(runtimeInstance.Context(), user)
        if nil != stateErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not look up the enrollment", stateErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, statusPayload{Enrolled: enrolled, Pending: pending}), nil
    }
}

/* VerifyHandler verifies a second factor for the caller's own enrollment: a TOTP code on X-2FA-Code against the stored secret, or a single-use recovery code on X-2FA-Recovery-Code redeemed atomically; 200 on success, 401 on a wrong or replayed factor. An accepted code is burned in the replay guard keyed on the normalized code, since Verify normalizes before comparing, under the nonce the framework's second-factor authenticator writes: handed the guard the sign-in reads, a code is spent once across both doors. A nil guard is one of this door's own. */
func VerifyHandler(storeSource store2fa.StoreSource, replayGuard melodysecuritycontract.NonceGuard, budget FactorBudget) melodyhttpcontract.Handler {
    if nil == replayGuard {
        replayGuard = melodysecurity.NewMemoryNonceGuard(melodysecurity.MemoryNonceGuardConfig{})
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

        factor, refusal := currentFactor(runtimeInstance, request, store, user, replayGuard, budget, func() melodyhttpcontract.Response {
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
    budget FactorBudget,
    absent func() melodyhttpcontract.Response,
) (string, melodyhttpcontract.Response) {
    factor, refusal := presentedFactor(runtimeInstance, request, store, user, replayGuard, budget, absent)
    if nil == refusal && nil != budget {
        budget.Release(user)
    }

    return factor, refusal
}

/* FactorBudget is the per-account allowance of presented second factors the sign-in spends too, so a session cannot walk the codes at these doors: Spend counts one, Release gives the allowance back once a factor was accepted. */
type FactorBudget interface {
    Spend(runtimeInstance melodyruntimecontract.Runtime, userIdentifier string) error

    Release(userIdentifier string)
}

/* refuseOverBudget counts the presented factor and answers the refusal of an account past its allowance, 429, or of a budget that cannot be read, 503. */
func refuseOverBudget(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, user string, budget FactorBudget) melodyhttpcontract.Response {
    if nil == budget {
        return nil
    }

    spendErr := budget.Spend(runtimeInstance, user)
    if nil == spendErr {
        return nil
    }

    if true == errors.Is(spendErr, examplesecurity.ErrSecondFactorBudgetSpent) {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusTooManyRequests, "too many attempts")
    }

    return unavailableStore(runtimeInstance, request, spendErr)
}

func presentedFactor(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    store *store2fa.Store,
    user string,
    replayGuard melodysecuritycontract.NonceGuard,
    budget FactorBudget,
    absent func() melodyhttpcontract.Response,
) (string, melodyhttpcontract.Response) {
    if "" != request.Header(melodysecurity.DefaultTotpRecoveryHeaderName) || "" != request.Header(melodysecurity.DefaultTotpCodeHeaderName) {
        if refusal := refuseOverBudget(runtimeInstance, request, user, budget); nil != refusal {
            return "", refusal
        }
    }

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

    if refusal := acceptTotpCode(runtimeInstance, request, user, secret, code, replayGuard); nil != refusal {
        return "", refusal
    }

    return factorTotp, nil
}

/* acceptTotpCode verifies a code against a secret and burns it in the replay guard under the nonce the sign-in writes, answering the refusal of a wrong or spent code */
func acceptTotpCode(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    user string,
    secret string,
    code string,
    replayGuard melodysecuritycontract.NonceGuard,
) melodyhttpcontract.Response {
    verified, verifyErr := totp.Verify(secret, code, totp.Config{})
    if nil != verifyErr {
        return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", verifyErr)
    }

    if false == verified {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "invalid code")
    }

    window, windowErr := totpCodeValidityWindow()
    if nil != windowErr {
        return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", windowErr)
    }

    seen, rememberErr := replayGuard.Remember(runtimeInstance, "2fa:"+user+":"+totp.NormalizeCode(code), bound.Of(window))
    if nil != rememberErr {
        return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not verify the code", rememberErr)
    }

    if true == seen {
        return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnauthorized, "code already used")
    }

    return nil
}

/* unavailableStore answers a door whose store could not be resolved with 503 and the cause journaled; the next request resolves the store again. */
func unavailableStore(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, storeErr error) melodyhttpcontract.Response {
    return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the second factor is unavailable", storeErr)
}

/* totpCodeValidityWindow is how long an accepted code stays verifiable, (2*skew+1) periods, and so how long a spent code stays burned; it resolves through the totp package, so it matches what Verify honours. */
func totpCodeValidityWindow() (time.Duration, error) {
    resolved, resolveErr := totp.Config{}.Resolve()
    if nil != resolveErr {
        return 0, resolveErr
    }

    return time.Duration(2*resolved.Skew+1) * resolved.Period, nil
}
