package security

import (
    "errors"
    "net/http/httptest"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
)

type fixedAuthenticator struct {
    token securitycontract.Token
}

func (instance *fixedAuthenticator) Supports(_ httpcontract.Request) bool {
    return true
}

func (instance *fixedAuthenticator) Authenticate(_ containercontract.Runtime, _ httpcontract.Request) (securitycontract.Token, error) {
    return instance.token, nil
}

type fixedEnrollmentStore struct {
    secret   string
    enrolled bool
}

func (instance *fixedEnrollmentStore) FindTotpSecret(
    _ containercontract.Runtime,
    _ string,
) (string, bool, error) {
    return instance.secret, instance.enrolled, nil
}

func totpRequest(codeHeaderValue string) httpcontract.Request {
    request := httptest.NewRequest("POST", "/login", nil)
    if "" != codeHeaderValue {
        request.Header.Set(DefaultTotpCodeHeaderName, codeHeaderValue)
    }

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

func totpAuthenticator(secret string, enrolled bool, guard securitycontract.NonceGuard) *TotpSecondFactorAuthenticator {
    return NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &fixedEnrollmentStore{secret: secret, enrolled: enrolled}, TotpSecondFactorAuthenticatorConfig{
        ReplayGuard:        guard,
        FailedAttemptStore: newRecordingFailedAttemptStore(),
    })
}

func TestTotpSecondFactor_NotEnrolledPassesPrimaryThrough(t *testing.T) {
    token, err := totpAuthenticator("", false, nil).Authenticate(withItsRuntime(totpRequest("")))
    if nil != err {
        t.Fatalf("authenticate: %v", err)
    }

    if false == token.IsAuthenticated() || "user-1" != token.UserIdentifier() {
        t.Fatal("expected the primary token to pass through when no second factor is configured")
    }
}

func TestTotpSecondFactor_EnrolledWithoutCodeIsPending(t *testing.T) {
    secret, _ := totp.GenerateSecret()

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(withItsRuntime(totpRequest("")))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an enrolled user without a code to be pending, not authenticated")
    }

    pendingUser, isPending := PendingUserFromToken(token)
    if false == isPending || "user-1" != pendingUser {
        t.Fatalf("expected a two-factor challenge for user-1, got present=%v user=%q", isPending, pendingUser)
    }

    if true == secondFactorRejected(t, token) {
        t.Fatal("expected a challenge the request never answered not to read as a rejected second factor")
    }
}

func TestTotpSecondFactor_AConfigOutsideItsRangesIsRefusedAtConstruction(t *testing.T) {
    for field, config := range map[string]totp.Options{
        "totp.Options.Period": {Period: bound.Of(25 * time.Hour)},
        "totp.Options.Digits": {Digits: bound.Of(9)},
        "totp.Options.Skew":   {Skew: bound.Of(11)},
    } {
        expectPanicNamingField(t, field, func() {
            NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &fixedEnrollmentStore{enrolled: false}, TotpSecondFactorAuthenticatorConfig{
                Totp:        config,
            })
        })
    }
}

func TestTotpSecondFactor_ATotpClockBesideTheAuthenticatorsIsRefused(t *testing.T) {
    var typedNilClock *clock.FrozenClock

    for name, totpClock := range map[string]clockcontract.Clock{
        "a clock":     clock.NewFrozenClock(time.Unix(1_000_000, 0)),
        "a typed nil": typedNilClock,
    } {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, "TotpSecondFactorAuthenticatorConfig.Totp.Clock", func() {
                NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &fixedEnrollmentStore{enrolled: false}, TotpSecondFactorAuthenticatorConfig{
                    Totp:        totp.Options{Clock: totpClock},
                })
            })
        })
    }
}

func TestTotpSecondFactor_ValidCodeAuthenticates(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Options{})

    token, err := totpAuthenticator(secret, true, nil).Authenticate(withItsRuntime(totpRequest(code)))
    if nil != err {
        t.Fatalf("authenticate: %v", err)
    }

    if false == token.IsAuthenticated() || "user-1" != token.UserIdentifier() {
        t.Fatal("expected a valid code to complete authentication")
    }
}

/* negative control: a wrong code keeps the request pending. */
func TestTotpSecondFactor_WrongCodeIsPending(t *testing.T) {
    secret, _ := totp.GenerateSecret()

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(withItsRuntime(totpRequest("000000")))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a wrong code to stay pending")
    }

    if false == secondFactorRejected(t, token) {
        t.Fatal("expected a wrong code to read as a rejected second factor")
    }
}

func TestTotpSecondFactor_ReplayedCodeIsRejected(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Options{})

    authenticator := totpAuthenticator(secret, true, NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    first, _ := authenticator.Authenticate(withItsRuntime(totpRequest(code)))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of a valid code to authenticate")
    }

    second, _ := authenticator.Authenticate(withItsRuntime(totpRequest(code)))
    if true == second.IsAuthenticated() {
        t.Fatal("expected a replayed code to be rejected by the guard")
    }

    if false == secondFactorRejected(t, second) {
        t.Fatal("expected a replayed code to read as a rejected second factor")
    }
}

/* Verify normalizes whitespace out of a submitted code, so "123 456" and "123456" are the same code. The replay guard must key on the normalized form: keying on the raw header value would let a captured code be replayed by re-spacing it. */
func TestTotpSecondFactor_ReplayedCodeIsRejectedWhenRespaced(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Options{})

    authenticator := totpAuthenticator(secret, true, NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    first, _ := authenticator.Authenticate(withItsRuntime(totpRequest(code)))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of a valid code to authenticate")
    }

    respaced := code[:3] + " " + code[3:]

    second, _ := authenticator.Authenticate(withItsRuntime(totpRequest(respaced)))
    if true == second.IsAuthenticated() {
        t.Fatalf("expected the respaced alias %q of an already-used code to be rejected by the guard", respaced)
    }
}

/* negative control: with no ReplayGuard configured the source defaults to an in-process guard, so a captured code still cannot be replayed out of the box. */
func TestTotpSecondFactor_ReplayedCodeIsRejectedByDefaultGuard(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Options{})

    authenticator := totpAuthenticator(secret, true, nil)

    first, _ := authenticator.Authenticate(withItsRuntime(totpRequest(code)))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of a valid code to authenticate")
    }

    second, _ := authenticator.Authenticate(withItsRuntime(totpRequest(code)))
    if true == second.IsAuthenticated() {
        t.Fatal("expected a replayed code to be rejected by the default in-process guard")
    }
}

/* recoveryEnrollmentStore also implements TwoFactorRecoveryStore, tracking each recovery code's unused state so a redeemed code cannot be redeemed a second time. */
type recoveryEnrollmentStore struct {
    secret   string
    enrolled bool
    unused   map[string]bool
}

func (instance *recoveryEnrollmentStore) FindTotpSecret(
    _ containercontract.Runtime,
    _ string,
) (string, bool, error) {
    return instance.secret, instance.enrolled, nil
}

func (instance *recoveryEnrollmentStore) RedeemRecoveryCode(
    _ containercontract.Runtime,
    _ string,
    code string,
) (bool, error) {
    if true == instance.unused[code] {
        instance.unused[code] = false

        return true, nil
    }

    return false, nil
}

func totpRecoveryRequest(recoveryValue string) httpcontract.Request {
    request := httptest.NewRequest("POST", "/login", nil)
    request.Header.Set(DefaultTotpRecoveryHeaderName, recoveryValue)

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

func totpAuthenticatorFor(store securitycontract.TwoFactorEnrollmentStore) *TotpSecondFactorAuthenticator {
    return NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, store, TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: newRecordingFailedAttemptStore()})
}

func TestTotpSecondFactor_RecoveryCodeAuthenticatesAndIsSingleUse(t *testing.T) {
    store := &recoveryEnrollmentStore{enrolled: true, unused: map[string]bool{"abcde-fghij": true}}
    authenticator := totpAuthenticatorFor(store)

    first, err := authenticator.Authenticate(withItsRuntime(totpRecoveryRequest("abcde-fghij")))
    if nil != err {
        t.Fatalf("authenticate: %v", err)
    }

    if false == first.IsAuthenticated() || "user-1" != first.UserIdentifier() {
        t.Fatal("expected a valid recovery code to complete authentication")
    }

    second, _ := authenticator.Authenticate(withItsRuntime(totpRecoveryRequest("abcde-fghij")))
    if true == second.IsAuthenticated() {
        t.Fatal("expected a consumed recovery code to be rejected on reuse")
    }
}

/* negative control: a recovery code that is not one of the user's unused codes keeps the request pending. */
func TestTotpSecondFactor_UnknownRecoveryCodeIsPending(t *testing.T) {
    store := &recoveryEnrollmentStore{enrolled: true, unused: map[string]bool{"abcde-fghij": true}}

    token, _ := totpAuthenticatorFor(store).Authenticate(withItsRuntime(totpRecoveryRequest("zzzzz-zzzzz")))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an unknown recovery code to stay pending")
    }

    if false == secondFactorRejected(t, token) {
        t.Fatal("expected a recovery code the store did not redeem to read as a rejected second factor")
    }
}

/* an enrollment store that does not implement TwoFactorRecoveryStore makes recovery unavailable: a recovery header is ignored and the request stays pending rather than authenticating. */
func TestTotpSecondFactor_RecoveryIgnoredWhenStoreUnsupported(t *testing.T) {
    secret, _ := totp.GenerateSecret()

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(withItsRuntime(totpRecoveryRequest("abcde-fghij")))

    if true == token.IsAuthenticated() {
        t.Fatal("expected recovery to be unavailable when the store does not support it")
    }

    if true == secondFactorRejected(t, token) {
        t.Fatal("expected the plain challenge, not the rejected form, when the store cannot read a recovery code")
    }
}

func TestTotpSecondFactor_ReplayWindowCoversEverySpanACodeVerifies(t *testing.T) {
    authenticator := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &fixedEnrollmentStore{enrolled: false}, TotpSecondFactorAuthenticatorConfig{
        Totp:        totp.Options{Period: bound.Of(60 * time.Second), Skew: bound.Of(10)},
    })

    if expected := 21 * 60 * time.Second; expected != authenticator.codeValidityWindow {
        t.Fatalf("expected the window to cover (2*10+1) periods (%v), got %v", expected, authenticator.codeValidityWindow)
    }
}

func TestTotpSecondFactor_AnonymousPrimaryPassesThrough(t *testing.T) {
    authenticator := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAnonymousToken()}, &fixedEnrollmentStore{enrolled: true}, TotpSecondFactorAuthenticatorConfig{})

    token, _ := authenticator.Authenticate(withItsRuntime(totpRequest("")))
    if true == token.IsAuthenticated() {
        t.Fatal("expected an anonymous primary result to stay anonymous")
    }

    if _, isPending := PendingUserFromToken(token); true == isPending {
        t.Fatal("expected no two-factor challenge when primary authentication did not succeed")
    }
}

/* the frozen instant sits decades from the real clock, so a code generated FOR that instant authenticates only if the authenticator verifies on the injected clock — and stops authenticating once that clock alone leaves the skew window. */
func TestTotpSecondFactor_VerifiesOnTheInjectedClock(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("secret: %v", secretErr)
    }

    frozen := clock.NewFrozenClock(time.Unix(1_000_000, 0))
    authenticator := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &fixedEnrollmentStore{secret: secret, enrolled: true}, TotpSecondFactorAuthenticatorConfig{
        Clock:              frozen,
        FailedAttemptStore: newRecordingFailedAttemptStore(),
    })

    code, codeErr := totp.GenerateCodeAt(secret, frozen.Now(), totp.Options{})
    if nil != codeErr {
        t.Fatalf("code: %v", codeErr)
    }

    token, authenticateErr := authenticator.Authenticate(withItsRuntime(totpRequest(code)))
    if nil != authenticateErr {
        t.Fatalf("authenticate: %v", authenticateErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatal("a code minted for the injected clock's instant was refused, so the authenticator read some other clock")
    }

    frozen.Advance(10 * time.Minute)

    lateToken, lateErr := authenticator.Authenticate(withItsRuntime(totpRequest(code)))
    if nil != lateErr {
        t.Fatalf("authenticate: %v", lateErr)
    }

    if true == lateToken.IsAuthenticated() {
        t.Fatal("the injected clock left the skew window and the code still authenticated, so the authenticator read some other clock")
    }
}

/* fixedEnrollmentStore never fails, so the fail-closed refusal needs a fixture that can reach it: inverted to return the primary token, that refusal would let every enrolled user past the second factor whenever the enrollment store is down, with every test built on fixedEnrollmentStore green. */
type failingEnrollmentStore struct {
    lookupErr error
}

func (instance *failingEnrollmentStore) FindTotpSecret(
    _ containercontract.Runtime,
    _ string,
) (string, bool, error) {
    return "", false, instance.lookupErr
}

func TestTotpSecondFactor_AFailingEnrollmentLookupRefusesInsteadOfPassingThrough(t *testing.T) {
    lookupErr := errors.New("enrollment store unavailable")

    authenticator := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &failingEnrollmentStore{lookupErr: lookupErr}, TotpSecondFactorAuthenticatorConfig{
        ReplayGuard: nil,
    })

    token, err := authenticator.Authenticate(withItsRuntime(totpRequest("")))
    if nil == err {
        t.Fatalf("expected an unavailable enrollment store to refuse rather than pass the primary token through")
    }

    if nil != token {
        t.Fatalf("expected no token to be handed out when the second factor cannot be decided, got %#v", token)
    }

    if false == errors.Is(err, lookupErr) {
        t.Fatalf("expected the store's own failure to stay classifiable beneath the refusal, got %v", err)
    }

    if "could not look up two-factor enrollment" != err.Error() {
        t.Fatalf("unexpected refusal message: %q", err.Error())
    }
}

/* secondFactorRejected reads the optional rejection door of a pending token, failing the test when the token does not carry it. */
func secondFactorRejected(t *testing.T, token securitycontract.Token) bool {
    t.Helper()

    rejection, reportsRejection := token.(securitycontract.TwoFactorRejection)
    if false == reportsRejection {
        t.Fatalf("expected a pending token reporting whether its second factor was rejected, got %T", token)
    }

    return rejection.SecondFactorRejected()
}

type failingRecoveryEnrollmentStore struct {
    redeemErr error
}

func (instance *failingRecoveryEnrollmentStore) FindTotpSecret(
    runtimeInstance containercontract.Runtime,
    userIdentifier string,
) (string, bool, error) {
    return "JBSWY3DPEHPK3PXP", true, nil
}

func (instance *failingRecoveryEnrollmentStore) RedeemRecoveryCode(
    runtimeInstance containercontract.Runtime,
    userIdentifier string,
    recoveryCode string,
) (bool, error) {
    return false, instance.redeemErr
}

func TestTotpSecondFactor_MarksItsThreeStoreFailures(t *testing.T) {
    storeErr := errors.New("redis is down")
    primary := &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}

    validCode, codeErr := totp.GenerateCodeAt("JBSWY3DPEHPK3PXP", time.Now(), totp.Options{})
    if nil != codeErr {
        t.Fatalf("unexpected error: %v", codeErr)
    }

    for name, turn := range map[string]struct {
        authenticator *TotpSecondFactorAuthenticator
        request       httpcontract.Request
    }{
        "the enrollment lookup": {
            authenticator: NewTotpSecondFactorAuthenticator(primary, &failingEnrollmentStore{lookupErr: storeErr}, TotpSecondFactorAuthenticatorConfig{}),
            request:       totpRequest(""),
        },
        "the recovery redemption": {
            authenticator: NewTotpSecondFactorAuthenticator(primary, &failingRecoveryEnrollmentStore{redeemErr: storeErr}, TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: newRecordingFailedAttemptStore()}),
            request:       totpRecoveryRequest("recovery-1"),
        },
        "the replay guard": {
            authenticator: totpAuthenticator("JBSWY3DPEHPK3PXP", true, &failingNonceGuard{failure: storeErr}),
            request:       totpRequest(validCode),
        },
    } {
        t.Run(name, func(t *testing.T) {
            token, err := turn.authenticator.Authenticate(withItsRuntime(turn.request))
            if nil != token || false == errors.Is(err, storeErr) || false == exception.IsInfrastructureFailure(err) {
                t.Fatalf("expected the store failure marked, got %v, %v", token, err)
            }
        })
    }

    wrongCode, wrongErr := totpAuthenticator("JBSWY3DPEHPK3PXP", true, &failingNonceGuard{failure: storeErr}).Authenticate(withItsRuntime(totpRequest("000000")))
    if nil != wrongErr || true == wrongCode.IsAuthenticated() || false == secondFactorRejected(t, wrongCode) {
        t.Fatalf("expected a wrong code to stay a rejected token with no error, got %v, %v", wrongCode, wrongErr)
    }
}

func TestTotpSecondFactor_ASecretThatCannotBeReadIsNoOutage(t *testing.T) {
    token, err := totpAuthenticator("not base32!", true, nil).Authenticate(withItsRuntime(totpRequest("123456")))
    if nil != token || nil == err || true == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected an unmarked refusal for an unreadable secret, got %v, %v", token, err)
    }
}

type failingPrimaryAuthenticator struct {
    err error
}

func (instance *failingPrimaryAuthenticator) Supports(_ httpcontract.Request) bool {
    return true
}

func (instance *failingPrimaryAuthenticator) Authenticate(_ containercontract.Runtime, _ httpcontract.Request) (securitycontract.Token, error) {
    return nil, instance.err
}

func TestTotpSecondFactor_ThePrimarysErrorPassesThroughUnmarked(t *testing.T) {
    primaryErr := errors.New("malformed credentials")

    _, err := NewTotpSecondFactorAuthenticator(&failingPrimaryAuthenticator{err: primaryErr}, &fixedEnrollmentStore{secret: "JBSWY3DPEHPK3PXP", enrolled: true}, TotpSecondFactorAuthenticatorConfig{}).Authenticate(withItsRuntime(totpRequest("")))
    if false == errors.Is(err, primaryErr) || true == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected the primary's error untouched, got %v", err)
    }
}

func TestTotpSecondFactor_AStoreFailureItsStoreFiledStaysFiled(t *testing.T) {
    filed := exception.NewError("enrollment store unavailable", nil, nil)
    _ = exception.MarkLogged(filed)

    _, filedErr := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &failingEnrollmentStore{lookupErr: filed}, TotpSecondFactorAuthenticatorConfig{}).Authenticate(withItsRuntime(totpRequest("")))
    if false == exception.IsAlreadyLogged(filedErr) || false == exception.IsInfrastructureFailure(filedErr) {
        t.Fatalf("expected a filed store failure to stay filed under the mark, got %v", filedErr)
    }

    _, unfiledErr := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, &failingEnrollmentStore{lookupErr: errors.New("enrollment store unavailable")}, TotpSecondFactorAuthenticatorConfig{}).Authenticate(withItsRuntime(totpRequest("")))
    if true == exception.IsAlreadyLogged(unfiledErr) {
        t.Fatalf("expected an unfiled store failure left to be filed, got %v", unfiledErr)
    }
}

func TestTotpSecondFactor_APrimaryThatCouldNotCheckTheCredentialIsPassedThrough(t *testing.T) {
    unavailable := &unavailableAuthenticatedToken{}

    token, err := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: unavailable}, &fixedEnrollmentStore{secret: "JBSWY3DPEHPK3PXP", enrolled: true}, TotpSecondFactorAuthenticatorConfig{}).Authenticate(withItsRuntime(totpRequest("")))
    if nil != err || securitycontract.Token(unavailable) != token {
        t.Fatalf("expected the unavailable token passed through, got %T, %v", token, err)
    }
}

func TestNewTotpSecondFactorAuthenticator_RefusesANilRequiredArgumentByName(t *testing.T) {
    cases := []struct {
        name        string
        primary     securitycontract.Authenticator
        enrollments securitycontract.TwoFactorEnrollmentStore
        argument    string
    }{
        {"a nil primary authenticator", nil, &fixedEnrollmentStore{}, "primary"},
        {"a nil enrollment store", &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, nil, "enrollments"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            expectPanicNamingTheArgument(t, testCase.argument, func() {
                NewTotpSecondFactorAuthenticator(testCase.primary, testCase.enrollments, TotpSecondFactorAuthenticatorConfig{})
            })
        })
    }
}

const throttleTestSecret = "JBSWY3DPEHPK3PXP"

/* countingRecoveryEnrollmentStore redeems through recoveryEnrollmentStore and counts every redemption it was asked for */
type countingRecoveryEnrollmentStore struct {
    recoveryEnrollmentStore
    mutex       sync.Mutex
    redemptions int
}

func (instance *countingRecoveryEnrollmentStore) RedeemRecoveryCode(runtimeInstance containercontract.Runtime, userIdentifier string, code string) (bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.redemptions++

    return instance.recoveryEnrollmentStore.RedeemRecoveryCode(runtimeInstance, userIdentifier, code)
}

/* headerUserAuthenticator accepts the user the X-User header names, so one authenticator serves several accounts */
type headerUserAuthenticator struct{}

func (instance *headerUserAuthenticator) Supports(_ httpcontract.Request) bool {
    return true
}

func (instance *headerUserAuthenticator) Authenticate(_ containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    return NewAuthenticatedToken(request.Header("X-User"), []string{"ROLE_USER"}, nil), nil
}

func throttledTotpAuthenticator(enrollments securitycontract.TwoFactorEnrollmentStore, config TotpSecondFactorAuthenticatorConfig) *TotpSecondFactorAuthenticator {
    return NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}, enrollments, config)
}

func throttleTestEnrollments() *countingRecoveryEnrollmentStore {
    return &countingRecoveryEnrollmentStore{recoveryEnrollmentStore: recoveryEnrollmentStore{secret: throttleTestSecret, enrolled: true, unused: map[string]bool{"recovery-1": true}}}
}

func throttleTestCode(t *testing.T, at time.Time) string {
    t.Helper()

    code, codeErr := totp.GenerateCodeAt(throttleTestSecret, at, totp.Options{})
    if nil != codeErr {
        t.Fatalf("code: %v", codeErr)
    }

    return code
}

func secondFactorLockOf(t *testing.T, token securitycontract.Token) (bool, time.Duration) {
    t.Helper()

    rejection, reportsRejection := token.(securitycontract.TwoFactorRejection)
    if false == reportsRejection {
        t.Fatalf("expected a token reporting its second factor, got %T", token)
    }

    return rejection.SecondFactorLocked(), rejection.SecondFactorRetryAfter()
}

func presentWrongCodes(t *testing.T, authenticator *TotpSecondFactorAuthenticator, runtimeInstance containercontract.Runtime, times int) {
    t.Helper()

    for attempt := 1; attempt <= times; attempt++ {
        token, err := authenticator.Authenticate(runtimeInstance, totpRequest("000000"))
        if nil != err || true == token.IsAuthenticated() {
            t.Fatalf("attempt %d: expected a refused code, got %v, %v", attempt, token, err)
        }
    }
}

func TestTotpSecondFactorAuthenticator_WrongCodesPastMaxFailedAttempts_RightCodeAnsweredLocked(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store})

    for attempt := 1; attempt <= 5; attempt++ {
        token, err := authenticator.Authenticate(nil, totpRequest("000000"))
        if nil != err || false == secondFactorRejected(t, token) {
            t.Fatalf("attempt %d: expected a rejected code, got %v, %v", attempt, token, err)
        }

        if locked, _ := secondFactorLockOf(t, token); true == locked {
            t.Fatalf("attempt %d: expected the code verified, not locked", attempt)
        }
    }

    token, err := authenticator.Authenticate(nil, totpRequest(throttleTestCode(t, time.Now())))
    if nil != err || true == token.IsAuthenticated() {
        t.Fatalf("expected the sixth factor refused, a right one included, got %v, %v", token, err)
    }

    if locked, _ := secondFactorLockOf(t, token); false == locked || false == secondFactorRejected(t, token) {
        t.Fatal("expected the sixth factor answered locked and rejected")
    }

    if bound.Of(5*time.Minute) != store.windows[0] {
        t.Fatalf("expected the default window of 5m, got %v", store.windows[0])
    }
}

func TestTotpSecondFactorAuthenticator_FifthAttemptStillVerified(t *testing.T) {
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: newRecordingFailedAttemptStore()})

    presentWrongCodes(t, authenticator, nil, 4)

    token, err := authenticator.Authenticate(nil, totpRequest(throttleTestCode(t, time.Now())))
    if nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the fifth factor verified and accepted, got %v, %v", token, err)
    }
}

func TestTotpSecondFactorAuthenticator_Locked_VerifiesNothing(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    store.countByKey[failedAttemptKey("user-1")] = 5
    enrollments := throttleTestEnrollments()
    guard := &runtimeRecordingNonceGuard{}
    authenticator := throttledTotpAuthenticator(enrollments, TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store, ReplayGuard: guard})

    codeToken, codeErr := authenticator.Authenticate(nil, totpRequest(throttleTestCode(t, time.Now())))
    recoveryToken, recoveryErr := authenticator.Authenticate(nil, totpRecoveryRequest("recovery-1"))
    if nil != codeErr || nil != recoveryErr {
        t.Fatalf("unexpected errors: %v, %v", codeErr, recoveryErr)
    }

    for name, token := range map[string]securitycontract.Token{"code": codeToken, "recovery": recoveryToken} {
        if locked, _ := secondFactorLockOf(t, token); false == locked {
            t.Fatalf("expected the %s answered locked", name)
        }
    }

    if 0 != len(guard.runtimes) || 0 != enrollments.redemptions || false == enrollments.unused["recovery-1"] {
        t.Fatalf("expected nothing verified, got %d replay guard writes and %d redemptions", len(guard.runtimes), enrollments.redemptions)
    }
}

func TestTotpSecondFactorAuthenticator_LockedToken_IsRejectedLockedAndCarriesTheRemainder(t *testing.T) {
    principal := NewAuthenticatedToken("user-1", nil, nil)

    locked := NewTwoFactorLockedToken(principal, 7*time.Second)
    if false == locked.SecondFactorRejected() || false == locked.SecondFactorLocked() || 7*time.Second != locked.SecondFactorRetryAfter() {
        t.Fatalf("expected rejected, locked and 7s, got %v %v %v", locked.SecondFactorRejected(), locked.SecondFactorLocked(), locked.SecondFactorRetryAfter())
    }

    if true == locked.IsAuthenticated() || "user-1" != locked.PendingUserIdentifier() {
        t.Fatal("expected the locked token pending for its principal and not authenticated")
    }

    rejected := NewTwoFactorRejectedToken(principal)
    if true == rejected.SecondFactorLocked() || 0 != rejected.SecondFactorRetryAfter() {
        t.Fatal("expected the rejected token not locked")
    }

    expectPanicNamingTheArgument(t, "retryAfter", func() {
        NewTwoFactorLockedToken(principal, -time.Second)
    })
}

func TestTotpSecondFactorAuthenticator_RetryAfter_RoundedUpAtLeastOneSecond(t *testing.T) {
    for _, row := range []struct {
        remaining time.Duration
        expected  time.Duration
    }{
        {200 * time.Millisecond, time.Second},
        {1200 * time.Millisecond, 2 * time.Second},
        {3 * time.Second, 3 * time.Second},
        {0, 5 * time.Minute},
    } {
        store := newRecordingFailedAttemptStore()
        store.countByKey[failedAttemptKey("user-1")] = 5
        store.windowRemaining = row.remaining

        token, err := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store}).Authenticate(nil, totpRequest("000000"))
        if nil != err {
            t.Fatalf("unexpected error: %v", err)
        }

        if locked, retryAfter := secondFactorLockOf(t, token); false == locked || row.expected != retryAfter {
            t.Fatalf("remaining %v: expected locked with %v, got %v %v", row.remaining, row.expected, locked, retryAfter)
        }
    }
}

func cacheThrottledAuthenticator(t *testing.T, frozen *clock.FrozenClock, config TotpSecondFactorAuthenticatorConfig) (*TotpSecondFactorAuthenticator, containercontract.Runtime) {
    t.Helper()

    cacheInstance := newTestCache(t, frozen)
    runtimeInstance, _ := runtimeWithCache(t, cacheInstance)

    config.Clock = frozen
    config.FailedAttemptStore = NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{Cache: cacheInstance, Clock: frozen})

    return throttledTotpAuthenticator(throttleTestEnrollments(), config), runtimeInstance
}

func TestTotpSecondFactorAuthenticator_WindowLapsed_VerifiesAgain(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    authenticator, runtimeInstance := cacheThrottledAuthenticator(t, frozen, TotpSecondFactorAuthenticatorConfig{})

    presentWrongCodes(t, authenticator, runtimeInstance, 6)

    frozen.Advance(5*time.Minute + time.Second)

    token, err := authenticator.Authenticate(runtimeInstance, totpRequest(throttleTestCode(t, frozen.Now())))
    if nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the factor verified once the window lapsed, got %v, %v", token, err)
    }
}

func TestTotpSecondFactorAuthenticator_WindowNotExtendedByLockedAttempts(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    authenticator, runtimeInstance := cacheThrottledAuthenticator(t, frozen, TotpSecondFactorAuthenticatorConfig{})

    presentWrongCodes(t, authenticator, runtimeInstance, 5)

    frozen.Advance(4 * time.Minute)

    lockedToken, lockedErr := authenticator.Authenticate(runtimeInstance, totpRequest(throttleTestCode(t, frozen.Now())))
    if nil != lockedErr {
        t.Fatalf("unexpected error: %v", lockedErr)
    }

    if locked, retryAfter := secondFactorLockOf(t, lockedToken); false == locked || time.Minute != retryAfter {
        t.Fatalf("expected locked with the 1m the window has left, got %v %v", locked, retryAfter)
    }

    frozen.Advance(time.Minute + time.Second)

    token, err := authenticator.Authenticate(runtimeInstance, totpRequest(throttleTestCode(t, frozen.Now())))
    if nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the window opened by the first refusal to lapse on time, got %v, %v", token, err)
    }
}

func TestTotpSecondFactorAuthenticator_AcceptedCode_ResetsTheCount(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store})

    presentWrongCodes(t, authenticator, nil, 4)

    if token, err := authenticator.Authenticate(nil, totpRequest(throttleTestCode(t, time.Now()))); nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the code accepted, got %v, %v", token, err)
    }

    if 1 != len(store.resets) || failedAttemptKey("user-1") != store.resets[0] {
        t.Fatalf("expected one reset of the account's count, got %v", store.resets)
    }

    presentWrongCodes(t, authenticator, nil, 5)

    token, _ := authenticator.Authenticate(nil, totpRequest("000000"))
    if locked, _ := secondFactorLockOf(t, token); false == locked {
        t.Fatal("expected the count started afresh after the acceptance, locking at the sixth")
    }
}

func TestTotpSecondFactorAuthenticator_ReplayedCode_Counts(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store})
    code := throttleTestCode(t, time.Now())

    if token, _ := authenticator.Authenticate(nil, totpRequest(code)); false == token.IsAuthenticated() {
        t.Fatal("expected the first use accepted")
    }

    replayed, _ := authenticator.Authenticate(nil, totpRequest(code))
    if false == secondFactorRejected(t, replayed) {
        t.Fatal("expected the replay rejected")
    }

    if 2 != store.incrementCount() || 1 != store.countByKey[failedAttemptKey("user-1")] || 1 != len(store.resets) {
        t.Fatalf("expected the replay counted and kept, got %d increments, count %d, %d resets", store.incrementCount(), store.countByKey[failedAttemptKey("user-1")], len(store.resets))
    }
}

func TestTotpSecondFactorAuthenticator_RecoveryCodes_ShareTheCount(t *testing.T) {
    enrollments := throttleTestEnrollments()
    authenticator := throttledTotpAuthenticator(enrollments, TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: newRecordingFailedAttemptStore()})

    presentWrongCodes(t, authenticator, nil, 3)

    for attempt := 1; attempt <= 2; attempt++ {
        if token, _ := authenticator.Authenticate(nil, totpRecoveryRequest("unknown")); false == secondFactorRejected(t, token) {
            t.Fatalf("recovery attempt %d: expected rejected", attempt)
        }
    }

    token, err := authenticator.Authenticate(nil, totpRecoveryRequest("recovery-1"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if locked, _ := secondFactorLockOf(t, token); false == locked || false == enrollments.unused["recovery-1"] {
        t.Fatal("expected the sixth factor, a valid recovery code, locked and not consumed")
    }
}

func TestTotpSecondFactorAuthenticator_AcceptedRecoveryCode_ResetsTheCount(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store})

    presentWrongCodes(t, authenticator, nil, 2)

    if token, err := authenticator.Authenticate(nil, totpRecoveryRequest("recovery-1")); nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the recovery code accepted, got %v, %v", token, err)
    }

    if 1 != len(store.resets) || 0 != store.countByKey[failedAttemptKey("user-1")] {
        t.Fatalf("expected the count reset, got %v resets and count %d", store.resets, store.countByKey[failedAttemptKey("user-1")])
    }
}

func TestTotpSecondFactorAuthenticator_NoFactorPresented_CountsNothing(t *testing.T) {
    store := newRecordingFailedAttemptStore()

    pending, _ := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store}).Authenticate(nil, totpRequest(""))
    if true == secondFactorRejected(t, pending) {
        t.Fatal("expected the plain challenge")
    }

    unreadRecovery, _ := throttledTotpAuthenticator(&fixedEnrollmentStore{secret: throttleTestSecret, enrolled: true}, TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store}).Authenticate(nil, totpRecoveryRequest("recovery-1"))
    if true == secondFactorRejected(t, unreadRecovery) {
        t.Fatal("expected the plain challenge for a recovery header the store can not read")
    }

    if 0 != store.incrementCount() {
        t.Fatalf("expected nothing counted, got %d", store.incrementCount())
    }
}

func TestTotpSecondFactorAuthenticator_NotEnrolled_CountsNothing(t *testing.T) {
    store := newRecordingFailedAttemptStore()

    token, _ := throttledTotpAuthenticator(&fixedEnrollmentStore{enrolled: false}, TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store}).Authenticate(nil, totpRequest("000000"))
    if false == token.IsAuthenticated() || 0 != store.incrementCount() {
        t.Fatalf("expected the primary token and nothing counted, got %v and %d", token, store.incrementCount())
    }
}

func TestTotpSecondFactorAuthenticator_PrimaryRefused_CountsNothing(t *testing.T) {
    store := newRecordingFailedAttemptStore()

    token, _ := NewTotpSecondFactorAuthenticator(&fixedAuthenticator{token: NewAnonymousToken()}, throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store}).Authenticate(nil, totpRequest("000000"))
    if true == token.IsAuthenticated() || 0 != store.incrementCount() {
        t.Fatalf("expected the refused primary passed through and nothing counted, got %v and %d", token, store.incrementCount())
    }
}

func TestTotpSecondFactorAuthenticator_CountIsPerAccount(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := NewTotpSecondFactorAuthenticator(&headerUserAuthenticator{}, throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store})

    request := func(user string, code string) httpcontract.Request {
        httpRequest := httptest.NewRequest("POST", "/login", nil)
        httpRequest.Header.Set("X-User", user)
        httpRequest.Header.Set(DefaultTotpCodeHeaderName, code)

        return testhelper.NewHttpTestRequestFromHttpRequest(httpRequest)
    }

    for attempt := 1; attempt <= 6; attempt++ {
        _, _ = authenticator.Authenticate(nil, request("user-a", "000000"))
    }

    token, err := authenticator.Authenticate(nil, request("user-b", throttleTestCode(t, time.Now())))
    if nil != err || false == token.IsAuthenticated() || "user-b" != token.UserIdentifier() {
        t.Fatalf("expected another account unaffected, got %v, %v", token, err)
    }
}

func TestTotpSecondFactorAuthenticator_UnlimitedMaxFailedAttempts_NeverCallsTheStore(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store, MaxFailedAttempts: bound.UnlimitedCount()})

    presentWrongCodes(t, authenticator, nil, 10)

    if token, err := authenticator.Authenticate(nil, totpRequest(throttleTestCode(t, time.Now()))); nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the code verified with the throttle off, got %v, %v", token, err)
    }

    if resetErr := authenticator.ResetFailedAttempts(nil, "user-1"); nil != resetErr {
        t.Fatalf("unexpected error: %v", resetErr)
    }

    if 0 != store.incrementCount() || 0 != len(store.resets) {
        t.Fatalf("expected the store never called, got %d increments and %d resets", store.incrementCount(), len(store.resets))
    }
}

func TestTotpSecondFactorAuthenticator_StoreFailure_AnsweredAsInfrastructureFailure(t *testing.T) {
    storeErr := errors.New("cache is down")
    guard := &runtimeRecordingNonceGuard{}

    token, err := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: &failingFailedAttemptStore{failure: storeErr}, ReplayGuard: guard}).Authenticate(nil, totpRequest(throttleTestCode(t, time.Now())))
    if nil != token || false == errors.Is(err, storeErr) || false == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected the store failure marked, got %v, %v", token, err)
    }

    if 0 != len(guard.runtimes) {
        t.Fatal("expected nothing verified once the count could not be taken")
    }
}

func TestTotpSecondFactorAuthenticator_ResetFailure_SignInStands_Journaled(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    store.resetErr = errors.New("cache is down")
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, err := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store}).Authenticate(runtimeInstance, totpRequest(throttleTestCode(t, time.Now())))
    if nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the sign-in to stand, got %v, %v", token, err)
    }

    warnings := logger.recordsAtLevel(loggingcontract.LevelWarning)
    if 1 != len(warnings) || "two-factor failed attempt reset failed" != warnings[0].message || 0 != len(logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatalf("expected one warning record, got %v", logger.records)
    }
}

func TestTotpSecondFactorAuthenticator_ResetFailedAttempts_Unlocks(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store})

    presentWrongCodes(t, authenticator, nil, 6)

    if resetErr := authenticator.ResetFailedAttempts(nil, "user-1"); nil != resetErr {
        t.Fatalf("unexpected error: %v", resetErr)
    }

    if token, err := authenticator.Authenticate(nil, totpRequest(throttleTestCode(t, time.Now()))); nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the unlocked account verified, got %v, %v", token, err)
    }

    store.resetErr = errors.New("cache is down")
    if resetErr := authenticator.ResetFailedAttempts(nil, "user-1"); false == errors.Is(resetErr, store.resetErr) {
        t.Fatalf("expected the store's failure answered, got %v", resetErr)
    }
}

func TestTotpSecondFactorAuthenticator_NilStore_CountsInTheRuntimeCache(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    cacheInstance := newTestCache(t, frozen)
    runtimeInstance, _ := runtimeWithCache(t, cacheInstance)

    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{Clock: frozen})

    presentWrongCodes(t, authenticator, runtimeInstance, 5)

    token, err := authenticator.Authenticate(runtimeInstance, totpRequest(throttleTestCode(t, frozen.Now())))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if locked, retryAfter := secondFactorLockOf(t, token); false == locked || 5*time.Minute != retryAfter {
        t.Fatalf("expected locked with the whole window left, got %v %v", locked, retryAfter)
    }

    var count int64
    if found, getErr := cacheInstance.Get(runtimeInstance, NewCacheFailedAttemptStore(CacheFailedAttemptStoreConfig{}).counterKey(failedAttemptKey("user-1")), &count); nil != getErr || false == found || 6 != count {
        t.Fatalf("expected six counted in the runtime's cache, got %v %d %v", found, count, getErr)
    }
}

func TestTotpSecondFactorAuthenticator_ConcurrentWrongCodes_AtMostMaxVerified(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    authenticator, runtimeInstance := cacheThrottledAuthenticator(t, frozen, TotpSecondFactorAuthenticatorConfig{})

    var verified atomic.Int64
    var group sync.WaitGroup
    for attempt := 0; attempt < 40; attempt++ {
        group.Add(1)
        go func() {
            defer group.Done()

            token, err := authenticator.Authenticate(runtimeInstance, totpRequest("000000"))
            if nil != err {
                t.Errorf("unexpected error: %v", err)

                return
            }

            if rejection, _ := token.(securitycontract.TwoFactorRejection); false == rejection.SecondFactorLocked() {
                verified.Add(1)
            }
        }()
    }
    group.Wait()

    if 5 != verified.Load() {
        t.Fatalf("expected exactly five codes verified, got %d", verified.Load())
    }
}

func TestNewTotpSecondFactorAuthenticator_MaxFailedAttemptsBelowMinimum_PanicsNamingField(t *testing.T) {
    expectPanicNamingField(t, "TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts", func() {
        throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{MaxFailedAttempts: bound.Of(0)})
    })
}

func TestNewTotpSecondFactorAuthenticator_LockoutWindowBelowMinimum_PanicsNamingField(t *testing.T) {
    expectPanicNamingField(t, "TotpSecondFactorAuthenticatorConfig.LockoutWindow", func() {
        throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{LockoutWindow: bound.Of(999 * time.Millisecond)})
    })
}

func TestNewTotpSecondFactorAuthenticator_LockoutWindowUnlimited_PanicsNamingField(t *testing.T) {
    expectPanicNamingField(t, "TotpSecondFactorAuthenticatorConfig.LockoutWindow", func() {
        throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{LockoutWindow: bound.UnlimitedDuration()})
    })
}

func TestNewTotpSecondFactorAuthenticator_TypedNilFailedAttemptStore_PanicsNamingField(t *testing.T) {
    var typedNil *CacheFailedAttemptStore

    expectPanicNamingField(t, "TotpSecondFactorAuthenticatorConfig.FailedAttemptStore", func() {
        throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: typedNil})
    })
}

func TestNewTotpSecondFactorAuthenticator_LimitsAtTheirMinimum_Admitted(t *testing.T) {
    store := newRecordingFailedAttemptStore()
    authenticator := throttledTotpAuthenticator(throttleTestEnrollments(), TotpSecondFactorAuthenticatorConfig{FailedAttemptStore: store, MaxFailedAttempts: bound.Of(1), LockoutWindow: bound.Of(time.Second)})

    presentWrongCodes(t, authenticator, nil, 1)

    token, _ := authenticator.Authenticate(nil, totpRequest("000000"))
    if locked, _ := secondFactorLockOf(t, token); false == locked || bound.Of(time.Second) != store.windows[1] {
        t.Fatalf("expected the second factor locked under a 1s window, got %v %v", locked, store.windows)
    }
}
