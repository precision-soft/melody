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

/* DefaultTotpRecoveryHeaderName is the request header a single-use recovery code is read from unless overridden. It is deliberately distinct from the TOTP code header so the two never collide — a TOTP code is 6 to 8 digits, a recovery code is the xxxxx-xxxxx form. */
const DefaultTotpRecoveryHeaderName = "X-2FA-Recovery-Code"

/* TotpSecondFactorAuthenticatorConfig composes a primary authenticator with a TOTP second factor into a single Authenticator, so it slots into the existing AuthenticatorManager without changing the manager's first-match flow. When the primary credential is accepted and the user has a TOTP enrollment, a valid code header is additionally required; otherwise the result is a non-authenticated TwoFactorPendingToken the application uses to prompt for a code.

   The ReplayGuard blocks reuse of an *accepted* code within its validity window, but this authenticator intentionally does NOT rate-limit *failed* code attempts: an unthrottled stream of distinct wrong guesses against a 6-digit code (with skew, a handful of codes are valid at any instant) can brute-force the second factor once the primary credential is known. The same caveat covers recovery codes — their larger space makes guessing far less feasible, but nothing here throttles wrong recovery guesses either. Throttling and per-user lockout are the application's responsibility and MUST front this authenticator — enforce them at the transport/middleware layer (an attempt counter with exponential backoff or a temporary lockout keyed on the authenticating user), the same layer that should already rate-limit primary-credential attempts.

   When the configured Enrollments store also implements TwoFactorRecoveryStore, a single-use recovery code supplied on RecoveryHeaderName is accepted as an alternative to a TOTP code: the store atomically verifies and consumes it, so each recovery code authenticates at most once. A store that does not implement that interface simply makes recovery codes unavailable. */
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

    /* default to an in-process replay guard (as the HMAC source does) so an accepted code cannot be replayed within its validity window out of the box; multi-instance deployments supply a shared guard. */
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

    /* no TOTP code and no accepted recovery code: the second factor is still outstanding, so surface a pending token the application uses to prompt for one rather than letting the primary credential stand on its own. */
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

/* tryRecoveryCode redeems a single-use recovery code when one is supplied on the recovery header and the enrollment store implements TwoFactorRecoveryStore. It reports whether a code was accepted (and thereby consumed): a false with a nil error means no recovery code was supplied, the store does not support recovery, or the supplied code was not a currently-unused one. Single use is enforced atomically by the store, so no replay-guard entry is recorded here. */
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

/* codeAlreadyUsed records an accepted code through the replay guard and reports whether it had already been used within its validity window. The constructor always installs a guard (an in-process one by default), so this relies on that invariant rather than tolerating a nil guard — a struct built by literal without one would fail loudly here instead of silently disabling replay protection.

   The nonce keys on the NORMALIZED code, exactly as Verify compared it: "123 456" and "123456" are the same code to Verify, so keying on the raw header value would let a captured code be replayed by re-spacing it. */
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
    /* resolve through the totp package so period and skew match exactly the values Verify accepts — in particular the maxSkew clamp: a raw read of a misconfigured (large) skew would remember an accepted code for far longer than it stays verifiable, pinning the entry in an in-process guard effectively forever while Verify only honors the clamped window. */
    resolved := instance.totpConfig.Resolve()

    period := uint64(resolved.Period)
    skew := uint64(resolved.Skew)

    /* the window must cover the whole span a code verifies — (2*skew+1) periods — so a replayed code stays blocked for as long as it would still be accepted. Compute in uint64 and saturate to the maximum duration on any overflow: a pathological period that wrapped time.Duration to a non-positive value would make the replay guard skip recording (a NonceGuard ignores a ttl <= 0) and silently disable replay protection. */
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
