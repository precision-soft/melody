package security

import (
    "math"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/security/totp"
)

/* DefaultTotpCodeHeaderName is the request header the TOTP code is read from unless overridden. */
const DefaultTotpCodeHeaderName = "X-2FA-Code"

/* DefaultTotpRecoveryHeaderName is the request header a single-use recovery code is read from unless overridden; it is distinct from the TOTP code header so the two never collide. */
const DefaultTotpRecoveryHeaderName = "X-2FA-Recovery-Code"

/* TotpSecondFactorAuthenticatorConfig composes a primary authenticator with a TOTP second factor into one Authenticator. When the primary credential is accepted and the user has a TOTP enrollment, a valid code header is also required; otherwise the result is a non-authenticated TwoFactorPendingToken the application uses to prompt for a code. When Enrollments also implements TwoFactorRecoveryStore, a single-use recovery code on RecoveryHeaderName is accepted instead of a TOTP code. The ReplayGuard blocks reuse of an accepted code, but failed attempts are NOT rate-limited: throttling and per-user lockout are the application's and MUST front this authenticator, or a 6-digit code can be brute-forced once the primary credential is known. */
type TotpSecondFactorAuthenticatorConfig struct {
    Primary securitycontract.Authenticator

    Enrollments securitycontract.TwoFactorEnrollmentStore

    /* CodeHeaderName overrides the header the TOTP code is read from; defaults to DefaultTotpCodeHeaderName. */
    CodeHeaderName string

    /* RecoveryHeaderName overrides the header a single-use recovery code is read from; defaults to DefaultTotpRecoveryHeaderName. It only takes effect when Enrollments also implements TwoFactorRecoveryStore. */
    RecoveryHeaderName string

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

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    var replayGuard securitycontract.NonceGuard = config.ReplayGuard
    if true == internal.IsNilInterface(replayGuard) {
        replayGuard = NewMemoryNonceGuardWithClock(clockInstance)
    }

    return &TotpSecondFactorAuthenticator{
        primary:            config.Primary,
        enrollments:        config.Enrollments,
        codeHeaderName:     codeHeaderName,
        recoveryHeaderName: recoveryHeaderName,
        totpConfig:         config.Totp,
        replayGuard:        replayGuard,
        clock:              clockInstance,
    }
}

type TotpSecondFactorAuthenticator struct {
    primary            securitycontract.Authenticator
    enrollments        securitycontract.TwoFactorEnrollmentStore
    codeHeaderName     string
    recoveryHeaderName string
    totpConfig         totp.Config
    replayGuard        securitycontract.NonceGuard
    clock              clockcontract.Clock
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

    runtimeInstance := request.RuntimeInstance()

    secret, enrolled, findErr := instance.enrollments.FindTotpSecret(runtimeInstance, token.UserIdentifier())
    if nil != findErr {
        return nil, exception.NewError("could not look up two-factor enrollment", nil, findErr)
    }

    if false == enrolled {
        return token, nil
    }

    code := request.Header(instance.codeHeaderName)
    if "" != code {
        return instance.authenticateWithTotpCode(request, token, secret, code)
    }

    if redeemed, recoveryErr := instance.tryRecoveryCode(request, token); nil != recoveryErr {
        return nil, recoveryErr
    } else if true == redeemed {
        return token, nil
    }

    /* no TOTP code and no accepted recovery code: the second factor is outstanding, so the primary credential does not stand on its own */
    return NewTwoFactorPendingToken(token), nil
}

/* authenticateWithTotpCode verifies a supplied TOTP code and, on success, enforces single use through the replay guard before authenticating. A wrong or replayed code yields a pending token so the caller re-prompts. */
func (instance *TotpSecondFactorAuthenticator) authenticateWithTotpCode(
    request httpcontract.Request,
    token securitycontract.Token,
    secret string,
    code string,
) (securitycontract.Token, error) {
    verified, verifyErr := totp.VerifyAt(secret, code, instance.clock.Now(), instance.totpConfig)
    if nil != verifyErr {
        return nil, exception.NewError("could not verify the two-factor code", nil, verifyErr)
    }

    if false == verified {
        return NewTwoFactorPendingToken(token), nil
    }

    if reused, replayErr := instance.codeAlreadyUsed(request, token.UserIdentifier(), code); nil != replayErr {
        return nil, replayErr
    } else if true == reused {
        return NewTwoFactorPendingToken(token), nil
    }

    return token, nil
}

/* tryRecoveryCode redeems a single-use recovery code supplied on the recovery header when the enrollment store implements TwoFactorRecoveryStore, and reports whether one was accepted and consumed. The store enforces single use atomically, so no replay-guard entry is recorded. */
func (instance *TotpSecondFactorAuthenticator) tryRecoveryCode(
    request httpcontract.Request,
    token securitycontract.Token,
) (bool, error) {
    recoveryStore, supportsRecovery := instance.enrollments.(securitycontract.TwoFactorRecoveryStore)
    if false == supportsRecovery {
        return false, nil
    }

    recoveryCode := request.Header(instance.recoveryHeaderName)
    if "" == recoveryCode {
        return false, nil
    }

    redeemed, redeemErr := recoveryStore.RedeemRecoveryCode(
        request.RuntimeInstance(),
        token.UserIdentifier(),
        recoveryCode,
    )
    if nil != redeemErr {
        return false, exception.NewError("could not redeem the two-factor recovery code", nil, redeemErr)
    }

    return redeemed, nil
}

/* codeAlreadyUsed records an accepted code through the replay guard and reports whether it was already used within its validity window. It relies on the constructor always installing a guard, so a literal without one fails loudly instead of disabling replay protection. The nonce keys on the normalized code, as Verify compares it, so a captured code cannot be replayed by re-spacing it. */
func (instance *TotpSecondFactorAuthenticator) codeAlreadyUsed(
    request httpcontract.Request,
    userIdentifier string,
    code string,
) (bool, error) {
    nonce := "2fa:" + userIdentifier + ":" + totp.NormalizeCode(code)

    seen, rememberErr := instance.replayGuard.Remember(request.RuntimeInstance(), nonce, instance.codeValidityWindow())
    if nil != rememberErr {
        return false, exception.NewError("two-factor replay guard failed", nil, rememberErr)
    }

    return seen, nil
}

func (instance *TotpSecondFactorAuthenticator) codeValidityWindow() time.Duration {
    /* resolved through the totp package so period and skew match what Verify accepts, the skew clamp included; a raw large skew would pin the entry far longer than the code verifies */
    resolved := instance.totpConfig.Resolve()

    period := uint64(resolved.Period)
    skew := uint64(resolved.Skew)

    /* the window covers every span a code verifies, (2*skew+1) periods, computed in uint64 and saturated: a period that wrapped time.Duration to a non-positive value would make the guard skip recording and disable replay protection */
    const maxSeconds = uint64(math.MaxInt64 / int64(time.Second))
    if skew > (maxSeconds-1)/2 {
        return time.Duration(math.MaxInt64)
    }

    steps := 2*skew + 1
    if period > maxSeconds/steps {
        return time.Duration(math.MaxInt64)
    }

    return time.Duration(period*steps) * time.Second
}

var _ securitycontract.Authenticator = (*TotpSecondFactorAuthenticator)(nil)
