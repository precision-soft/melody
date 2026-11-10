package security

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
)

/* DefaultTotpCodeHeaderName is the request header the TOTP code is read from unless overridden. */
const DefaultTotpCodeHeaderName = "X-2FA-Code"

/* DefaultTotpRecoveryHeaderName is the request header a single-use recovery code is read from unless overridden; it is distinct from the TOTP code header so the two never collide. */
const DefaultTotpRecoveryHeaderName = "X-2FA-Recovery-Code"

/* TotpSecondFactorAuthenticatorConfig composes a primary authenticator with a TOTP second factor into one Authenticator. When the primary credential is accepted and the user has a TOTP enrollment, a valid code header is also required; otherwise the result is a non-authenticated TwoFactorPendingToken the application uses to prompt for a code. When Enrollments also implements TwoFactorRecoveryStore, a single-use recovery code on RecoveryHeaderName is accepted instead of a TOTP code. The ReplayGuard blocks reuse of an accepted code, but failed attempts are NOT rate-limited: throttling and per-user lockout are the application's and MUST front this authenticator, or a 6-digit code can be brute-forced once the primary credential is known. An enrollment lookup, a recovery redemption or a replay guard that fails returns its error marked with exception.MarkInfrastructureFailure, so the firewall answers it as a credential nobody could check rather than a failed sign-in. */
type TotpSecondFactorAuthenticatorConfig struct {
    Primary securitycontract.Authenticator

    Enrollments securitycontract.TwoFactorEnrollmentStore

    /* CodeHeaderName overrides the header the TOTP code is read from; defaults to DefaultTotpCodeHeaderName. */
    CodeHeaderName string

    /* RecoveryHeaderName overrides the header a single-use recovery code is read from; defaults to DefaultTotpRecoveryHeaderName. It only takes effect when Enrollments also implements TwoFactorRecoveryStore. */
    RecoveryHeaderName string

    /* Totp tunes the algorithm; a config outside its ranges is refused at construction naming the field. Its Clock is refused: the authenticator's Clock is the clock the code is verified against. */
    Totp totp.Config

    /* ReplayGuard enforces single use of an accepted code within its validity window (the same NonceGuard contract the HMAC source uses). Optional: defaults to an in-process guard, so supply a shared one for multi-instance deployments. */
    ReplayGuard securitycontract.NonceGuard

    /* Clock is the clock the code is verified against; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock
}

func NewTotpSecondFactorAuthenticator(config TotpSecondFactorAuthenticatorConfig) *TotpSecondFactorAuthenticator {
    if true == internal.IsNilInterface(config.Primary) {
        exception.Panic(exception.NewError("totp second factor primary authenticator is nil", nil, nil))
    }

    if true == internal.IsNilInterface(config.Enrollments) {
        exception.Panic(exception.NewError("totp second factor enrollment store is nil", nil, nil))
    }

    codeHeaderName := config.CodeHeaderName
    if "" == codeHeaderName {
        codeHeaderName = DefaultTotpCodeHeaderName
    }

    recoveryHeaderName := config.RecoveryHeaderName
    if "" == recoveryHeaderName {
        recoveryHeaderName = DefaultTotpRecoveryHeaderName
    }

    if nil != config.Totp.Clock {
        exception.Panic(
            exception.NewError(
                "the authenticator's Clock is the clock the code is verified against",
                exceptioncontract.Context{
                    "field": "TotpSecondFactorAuthenticatorConfig.Totp.Clock",
                },
                nil,
            ),
        )
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    totpConfig := config.Totp
    totpConfig.Clock = clockInstance

    totpResolved, resolveErr := totpConfig.Resolve()
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    var replayGuard securitycontract.NonceGuard = config.ReplayGuard
    if true == internal.IsNilInterface(replayGuard) {
        replayGuard = NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: clockInstance})
    }

    return &TotpSecondFactorAuthenticator{
        primary:            config.Primary,
        enrollments:        config.Enrollments,
        codeHeaderName:     codeHeaderName,
        recoveryHeaderName: recoveryHeaderName,
        totpConfig:         totpConfig,
        codeValidityWindow: time.Duration(2*totpResolved.Skew+1) * totpResolved.Period,
        replayGuard:        replayGuard,
    }
}

type TotpSecondFactorAuthenticator struct {
    primary            securitycontract.Authenticator
    enrollments        securitycontract.TwoFactorEnrollmentStore
    codeHeaderName     string
    recoveryHeaderName string
    totpConfig         totp.Config
    codeValidityWindow time.Duration
    replayGuard        securitycontract.NonceGuard
}

func (instance *TotpSecondFactorAuthenticator) Supports(request httpcontract.Request) bool {
    return instance.primary.Supports(request)
}

func (instance *TotpSecondFactorAuthenticator) Authenticate(request httpcontract.Request) (securitycontract.Token, error) {
    token, authenticateErr := instance.primary.Authenticate(request)
    if nil != authenticateErr {
        return nil, authenticateErr
    }

    if true == internal.IsNilInterface(token) || false == token.IsAuthenticated() {
        return token, nil
    }

    /* a primary that could not check the credential has nothing for the second factor to complete */
    if _, unavailable := AuthenticationUnavailableCause(token); true == unavailable {
        return token, nil
    }

    runtimeInstance := request.RuntimeInstance()

    secret, enrolled, findErr := instance.enrollments.FindTotpSecret(runtimeInstance, token.UserIdentifier())
    if nil != findErr {
        return nil, storeFailure("could not look up two-factor enrollment", findErr)
    }

    if false == enrolled {
        return token, nil
    }

    code := request.Header(instance.codeHeaderName)
    if "" != code {
        return instance.authenticateWithTotpCode(request, token, secret, code)
    }

    if redeemed, supplied, recoveryErr := instance.tryRecoveryCode(request, token); nil != recoveryErr {
        return nil, recoveryErr
    } else if true == redeemed {
        return token, nil
    } else if true == supplied {
        return NewTwoFactorRejectedToken(token), nil
    }

    /* no TOTP code and no recovery code: the second factor is outstanding, so the primary credential does not stand on its own */
    return NewTwoFactorPendingToken(token), nil
}

/* authenticateWithTotpCode verifies a supplied TOTP code and, on success, enforces single use through the replay guard before authenticating. A wrong or replayed code yields a rejected pending token so the caller re-prompts and the refusal is announced as a login failure. */
func (instance *TotpSecondFactorAuthenticator) authenticateWithTotpCode(
    request httpcontract.Request,
    token securitycontract.Token,
    secret string,
    code string,
) (securitycontract.Token, error) {
    verified, verifyErr := totp.Verify(secret, code, instance.totpConfig)
    if nil != verifyErr {
        return nil, exception.NewError("could not verify the two-factor code", nil, verifyErr)
    }

    if false == verified {
        return NewTwoFactorRejectedToken(token), nil
    }

    if reused, replayErr := instance.codeAlreadyUsed(request, token.UserIdentifier(), code); nil != replayErr {
        return nil, replayErr
    } else if true == reused {
        return NewTwoFactorRejectedToken(token), nil
    }

    return token, nil
}

/* tryRecoveryCode redeems a single-use recovery code supplied on the recovery header when the enrollment store implements TwoFactorRecoveryStore, and reports whether one was accepted and consumed and whether one was supplied. A store without that door answers neither: the header is not read, so the caller answers the plain challenge rather than the rejected form. The store enforces single use atomically, so no replay-guard entry is recorded. */
func (instance *TotpSecondFactorAuthenticator) tryRecoveryCode(
    request httpcontract.Request,
    token securitycontract.Token,
) (bool, bool, error) {
    recoveryStore, supportsRecovery := instance.enrollments.(securitycontract.TwoFactorRecoveryStore)
    if false == supportsRecovery {
        return false, false, nil
    }

    recoveryCode := request.Header(instance.recoveryHeaderName)
    if "" == recoveryCode {
        return false, false, nil
    }

    redeemed, redeemErr := recoveryStore.RedeemRecoveryCode(
        request.RuntimeInstance(),
        token.UserIdentifier(),
        recoveryCode,
    )
    if nil != redeemErr {
        return false, true, storeFailure("could not redeem the two-factor recovery code", redeemErr)
    }

    return redeemed, true, nil
}

/* codeAlreadyUsed records an accepted code through the replay guard and reports whether it was already used within its validity window. It relies on the constructor always installing a guard, so a literal without one fails loudly instead of disabling replay protection. The nonce keys on the normalized code, as Verify compares it, so a captured code cannot be replayed by re-spacing it. */
func (instance *TotpSecondFactorAuthenticator) codeAlreadyUsed(
    request httpcontract.Request,
    userIdentifier string,
    code string,
) (bool, error) {
    nonce := "2fa:" + userIdentifier + ":" + totp.NormalizeCode(code)

    seen, rememberErr := instance.replayGuard.Remember(request.RuntimeInstance(), nonce, bound.Of(instance.codeValidityWindow))
    if nil != rememberErr {
        return false, storeFailure("two-factor replay guard failed", rememberErr)
    }

    return seen, nil
}

/* storeFailure marks a store's failure under the message; a cause its store already filed keeps the wrap marked logged, so the store's record stays the only one */
func storeFailure(message string, storeErr error) error {
    failure := exception.NewError(message, nil, exception.MarkInfrastructureFailure(storeErr))
    if true == exception.IsAlreadyLogged(storeErr) {
        _ = exception.MarkLogged(failure)
    }

    return failure
}

var _ securitycontract.Authenticator = (*TotpSecondFactorAuthenticator)(nil)
