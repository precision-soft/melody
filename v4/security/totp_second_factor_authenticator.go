package security

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
)

/* DefaultTotpCodeHeaderName is the request header the TOTP code is read from unless overridden. */
const DefaultTotpCodeHeaderName = "X-2FA-Code"

/* DefaultTotpRecoveryHeaderName is the request header a single-use recovery code is read from unless overridden; it is distinct from the TOTP code header so the two never collide. */
const DefaultTotpRecoveryHeaderName = "X-2FA-Recovery-Code"

/* TotpSecondFactorAuthenticatorConfig tunes how NewTotpSecondFactorAuthenticator composes its primary authenticator with a TOTP second factor into one Authenticator. When the primary credential is accepted and the user has a TOTP enrollment, a valid code header is also required; otherwise the result is a non-authenticated TwoFactorPendingToken the application uses to prompt for a code. When the enrollments store also implements TwoFactorRecoveryStore, a single-use recovery code on RecoveryHeaderName is accepted instead of a TOTP code. The ReplayGuard blocks reuse of an accepted code. Every presented factor, a TOTP code or a recovery code, is counted per account before it is verified, and past MaxFailedAttempts inside LockoutWindow the factor is refused unread as the locked token of NewTwoFactorLockedToken, answered 429 with Retry-After; an accepted factor resets the count. Whoever holds the password can keep the account's owner out for the window; ResetFailedAttempts unlocks it. An enrollment lookup, a recovery redemption, a replay guard or a failed attempt store that fails returns its error marked with exception.MarkInfrastructureFailure, so the firewall answers it as a credential nobody could check rather than a failed sign-in. */
type TotpSecondFactorAuthenticatorConfig struct {
    /* CodeHeaderName overrides the header the TOTP code is read from; defaults to DefaultTotpCodeHeaderName. */
    CodeHeaderName string

    /* RecoveryHeaderName overrides the header a single-use recovery code is read from; defaults to DefaultTotpRecoveryHeaderName. It only takes effect when the enrollments store also implements TwoFactorRecoveryStore. */
    RecoveryHeaderName string

    /* Totp tunes the algorithm; a config outside its ranges is refused at construction naming the field. Its Clock is refused: the authenticator's Clock is the clock the code is verified against. */
    Totp totp.Options

    /* ReplayGuard enforces single use of an accepted code within its validity window (the same NonceGuard contract the HMAC source uses). Optional: defaults to an in-process guard, so supply a shared one for multi-instance deployments. */
    ReplayGuard securitycontract.NonceGuard

    /* Clock is the clock the code is verified against; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock

    /* MaxFailedAttempts is how many presented factors, TOTP codes and recovery codes together, an account may have refused inside LockoutWindow before the authenticator stops verifying: 5 by default, at least 1. bound.UnlimitedCount() turns the throttle off and nothing is counted, for an application whose own throttle fronts this authenticator. */
    MaxFailedAttempts bound.Count

    /* LockoutWindow is the span the presented factors are counted over, opened by the first one and never extended: 5m by default, at least 1s. Unlimited is refused, since a count that never lapses locks its account for good: a long window is the door. */
    LockoutWindow bound.Duration

    /* FailedAttemptStore is where the count lives; nil is a CacheFailedAttemptStore over the application's cache, shared between replicas exactly when that cache is. */
    FailedAttemptStore securitycontract.FailedAttemptStore
}

var totpMaxFailedAttemptsRule = bound.Rule[int]{
    Name:              "TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts",
    Default:           5,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var totpLockoutWindowRule = bound.Rule[time.Duration]{
    Name:    "TotpSecondFactorAuthenticatorConfig.LockoutWindow",
    Default: 5 * time.Minute,
    Minimum: bound.TtlMinimum,
}

/* NewTotpSecondFactorAuthenticator requires a TOTP code from the users enrollments holds once primary accepted them; a nil primary or enrollments panics naming the argument, a limit outside its rule or a typed nil FailedAttemptStore naming the field. */
func NewTotpSecondFactorAuthenticator(primary securitycontract.Authenticator, enrollments securitycontract.TwoFactorEnrollmentStore, config TotpSecondFactorAuthenticatorConfig) *TotpSecondFactorAuthenticator {
    if true == internal.IsNilInterface(primary) {
        exception.Panic(exception.NewError("totp second factor primary authenticator is nil", exceptioncontract.Context{"argument": "primary"}, nil))
    }

    if true == internal.IsNilInterface(enrollments) {
        exception.Panic(exception.NewError("totp second factor enrollment store is nil", exceptioncontract.Context{"argument": "enrollments"}, nil))
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

    clockInstance, clockErr := clock.ResolveOptionalClock(config.Clock, "totp second factor authenticator", "TotpSecondFactorAuthenticatorConfig.Clock")
    if nil != clockErr {
        exception.Panic(exception.FromError(clockErr))
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

    maxFailedAttempts, maxFailedAttemptsErr := bound.Resolve(config.MaxFailedAttempts, totpMaxFailedAttemptsRule)
    if nil != maxFailedAttemptsErr {
        exception.Panic(exception.FromError(maxFailedAttemptsErr))
    }

    lockoutWindow, lockoutWindowErr := bound.Resolve(config.LockoutWindow, totpLockoutWindowRule)
    if nil != lockoutWindowErr {
        exception.Panic(exception.FromError(lockoutWindowErr))
    }

    refuseTypedNilField(
        "totp second factor failed attempt store is a typed nil",
        "TotpSecondFactorAuthenticatorConfig.FailedAttemptStore",
        "NewTotpSecondFactorAuthenticator",
        config.FailedAttemptStore,
    )

    var failedAttemptStore securitycontract.FailedAttemptStore
    if false == maxFailedAttempts.Unlimited {
        failedAttemptStore = config.FailedAttemptStore
        if nil == failedAttemptStore {
            failedAttemptStore = NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{Clock: clockInstance})
        }
    }

    return &TotpSecondFactorAuthenticator{
        primary:            primary,
        enrollments:        enrollments,
        codeHeaderName:     codeHeaderName,
        recoveryHeaderName: recoveryHeaderName,
        totpConfig:         totpConfig,
        codeValidityWindow: time.Duration(2*totpResolved.Skew+1) * totpResolved.Period,
        replayGuard:        replayGuard,
        maxFailedAttempts:  maxFailedAttempts.Value,
        lockoutWindow:      lockoutWindow.Value,
        failedAttemptStore: failedAttemptStore,
    }
}

type TotpSecondFactorAuthenticator struct {
    primary            securitycontract.Authenticator
    enrollments        securitycontract.TwoFactorEnrollmentStore
    codeHeaderName     string
    recoveryHeaderName string
    totpConfig         totp.Options
    codeValidityWindow time.Duration
    replayGuard        securitycontract.NonceGuard
    maxFailedAttempts  int
    lockoutWindow      time.Duration

    /* nil when the throttle is off */
    failedAttemptStore securitycontract.FailedAttemptStore
}

func (instance *TotpSecondFactorAuthenticator) Supports(request httpcontract.Request) bool {
    return instance.primary.Supports(request)
}

func (instance *TotpSecondFactorAuthenticator) Authenticate(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    token, authenticateErr := instance.primary.Authenticate(runtimeInstance, request)
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

    secret, enrolled, findErr := instance.enrollments.FindTotpSecret(runtimeInstance, token.UserIdentifier())
    if nil != findErr {
        return nil, storeFailure("could not look up two-factor enrollment", findErr)
    }

    if false == enrolled {
        return token, nil
    }

    if false == instance.presentsFactor(request) {
        /* no TOTP code and no recovery code: the second factor is outstanding, so the primary credential does not stand on its own */
        return NewTwoFactorPendingToken(token), nil
    }

    /* counted before it is verified, in one step, so concurrent guesses can not all read a count below the allowance */
    if lockedToken, countErr := instance.countPresentedFactor(runtimeInstance, token); nil != countErr {
        return nil, countErr
    } else if nil != lockedToken {
        return lockedToken, nil
    }

    verifiedToken, verifyErr := instance.verifyPresentedFactor(runtimeInstance, request, token, secret)
    if nil != verifyErr {
        return nil, verifyErr
    }

    if true == verifiedToken.IsAuthenticated() {
        instance.forgetFailedAttempts(runtimeInstance, token.UserIdentifier())
    }

    return verifiedToken, nil
}

/* ResetFailedAttempts forgets the refused factors counted for the user, unlocking an account the throttle locked; with the throttle off there is nothing to forget. */
func (instance *TotpSecondFactorAuthenticator) ResetFailedAttempts(runtimeInstance containercontract.Runtime, userIdentifier string) error {
    if nil == instance.failedAttemptStore {
        return nil
    }

    return instance.failedAttemptStore.Reset(runtimeInstance, failedAttemptKey(userIdentifier))
}

func (instance *TotpSecondFactorAuthenticator) verifyPresentedFactor(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    token securitycontract.Token,
    secret string,
) (securitycontract.Token, error) {
    code := request.Header(instance.codeHeaderName)
    if "" != code {
        return instance.authenticateWithTotpCode(runtimeInstance, request, token, secret, code)
    }

    redeemed, _, recoveryErr := instance.tryRecoveryCode(runtimeInstance, request, token)
    if nil != recoveryErr {
        return nil, recoveryErr
    }

    if true == redeemed {
        return token, nil
    }

    return NewTwoFactorRejectedToken(token), nil
}

/* presentsFactor reports a TOTP code, or a recovery code on a store that redeems one; a recovery header the store can not read is no factor */
func (instance *TotpSecondFactorAuthenticator) presentsFactor(request httpcontract.Request) bool {
    if "" != request.Header(instance.codeHeaderName) {
        return true
    }

    if _, supportsRecovery := instance.enrollments.(securitycontract.TwoFactorRecoveryStore); false == supportsRecovery {
        return false
    }

    return "" != request.Header(instance.recoveryHeaderName)
}

/* countPresentedFactor answers the locked token once the account is past its allowance, nil while the factor may be verified */
func (instance *TotpSecondFactorAuthenticator) countPresentedFactor(
    runtimeInstance containercontract.Runtime,
    token securitycontract.Token,
) (securitycontract.Token, error) {
    if nil == instance.failedAttemptStore {
        return nil, nil
    }

    count, incrementErr := instance.failedAttemptStore.Increment(
        runtimeInstance,
        failedAttemptKey(token.UserIdentifier()),
        bound.Of(instance.lockoutWindow),
    )
    if nil != incrementErr {
        return nil, storeFailure("two-factor failed attempt store failed", incrementErr)
    }

    if count.Count <= instance.maxFailedAttempts {
        return nil, nil
    }

    return NewTwoFactorLockedToken(token, lockoutRetryAfter(count.WindowRemaining, instance.lockoutWindow)), nil
}

/* forgetFailedAttempts never undoes the sign-in it follows: a count it could not reset lapses with its window */
func (instance *TotpSecondFactorAuthenticator) forgetFailedAttempts(runtimeInstance containercontract.Runtime, userIdentifier string) {
    if nil == instance.failedAttemptStore {
        return
    }

    resetErr := instance.failedAttemptStore.Reset(runtimeInstance, failedAttemptKey(userIdentifier))
    if nil == resetErr {
        return
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Warning(
            "two-factor failed attempt reset failed",
            exception.LogContext(resetErr, exception.LogContextOptions{}),
        )
    }
}

func failedAttemptKey(userIdentifier string) string {
    return "2fa:" + userIdentifier
}

/* lockoutRetryAfter rounds the remainder up to a whole second, and answers the whole window when the store could not say */
func lockoutRetryAfter(windowRemaining time.Duration, lockoutWindow time.Duration) time.Duration {
    if 0 >= windowRemaining {
        return lockoutWindow
    }

    return (windowRemaining + time.Second - 1) / time.Second * time.Second
}

/* authenticateWithTotpCode verifies a supplied TOTP code and, on success, enforces single use through the replay guard before authenticating. A wrong or replayed code yields a rejected pending token so the caller re-prompts and the refusal is announced as a login failure. */
func (instance *TotpSecondFactorAuthenticator) authenticateWithTotpCode(
    runtimeInstance containercontract.Runtime,
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

    if reused, replayErr := instance.codeAlreadyUsed(runtimeInstance, token.UserIdentifier(), code); nil != replayErr {
        return nil, replayErr
    } else if true == reused {
        return NewTwoFactorRejectedToken(token), nil
    }

    return token, nil
}

/* tryRecoveryCode redeems a single-use recovery code supplied on the recovery header when the enrollment store implements TwoFactorRecoveryStore, and reports whether one was accepted and consumed and whether one was supplied. A store without that door answers neither: the header is not read, so the caller answers the plain challenge rather than the rejected form. The store enforces single use atomically, so no replay-guard entry is recorded. */
func (instance *TotpSecondFactorAuthenticator) tryRecoveryCode(
    runtimeInstance containercontract.Runtime,
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
        runtimeInstance,
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
    runtimeInstance containercontract.Runtime,
    userIdentifier string,
    code string,
) (bool, error) {
    nonce := "2fa:" + userIdentifier + ":" + totp.NormalizeCode(code)

    seen, rememberErr := instance.replayGuard.Remember(runtimeInstance, nonce, bound.Of(instance.codeValidityWindow))
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
