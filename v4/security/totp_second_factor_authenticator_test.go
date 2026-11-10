package security

import (
    "errors"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
)

type fixedAuthenticator struct {
    token securitycontract.Token
}

func (instance *fixedAuthenticator) Supports(_ httpcontract.Request) bool {
    return true
}

func (instance *fixedAuthenticator) Authenticate(_ httpcontract.Request) (securitycontract.Token, error) {
    return instance.token, nil
}

type fixedEnrollmentStore struct {
    secret   string
    enrolled bool
}

func (instance *fixedEnrollmentStore) FindTotpSecret(
    _ runtimecontract.Runtime,
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
    return NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
        Enrollments: &fixedEnrollmentStore{secret: secret, enrolled: enrolled},
        ReplayGuard: guard,
    })
}

func TestTotpSecondFactor_NotEnrolledPassesPrimaryThrough(t *testing.T) {
    token, err := totpAuthenticator("", false, nil).Authenticate(totpRequest(""))
    if nil != err {
        t.Fatalf("authenticate: %v", err)
    }

    if false == token.IsAuthenticated() || "user-1" != token.UserIdentifier() {
        t.Fatal("expected the primary token to pass through when no second factor is configured")
    }
}

func TestTotpSecondFactor_EnrolledWithoutCodeIsPending(t *testing.T) {
    secret, _ := totp.GenerateSecret()

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(totpRequest(""))

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
    for field, config := range map[string]totp.Config{
        "totp.Config.Period": {Period: bound.Of(25 * time.Hour)},
        "totp.Config.Digits": {Digits: bound.Of(9)},
        "totp.Config.Skew":   {Skew: bound.Of(11)},
    } {
        expectPanicNamingField(t, field, func() {
            NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
                Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
                Enrollments: &fixedEnrollmentStore{enrolled: false},
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
                NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
                    Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
                    Enrollments: &fixedEnrollmentStore{enrolled: false},
                    Totp:        totp.Config{Clock: totpClock},
                })
            })
        })
    }
}

func TestTotpSecondFactor_ValidCodeAuthenticates(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})

    token, err := totpAuthenticator(secret, true, nil).Authenticate(totpRequest(code))
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

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(totpRequest("000000"))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a wrong code to stay pending")
    }

    if false == secondFactorRejected(t, token) {
        t.Fatal("expected a wrong code to read as a rejected second factor")
    }
}

func TestTotpSecondFactor_ReplayedCodeIsRejected(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})

    authenticator := totpAuthenticator(secret, true, NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    first, _ := authenticator.Authenticate(totpRequest(code))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of a valid code to authenticate")
    }

    second, _ := authenticator.Authenticate(totpRequest(code))
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
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})

    authenticator := totpAuthenticator(secret, true, NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    first, _ := authenticator.Authenticate(totpRequest(code))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of a valid code to authenticate")
    }

    respaced := code[:3] + " " + code[3:]

    second, _ := authenticator.Authenticate(totpRequest(respaced))
    if true == second.IsAuthenticated() {
        t.Fatalf("expected the respaced alias %q of an already-used code to be rejected by the guard", respaced)
    }
}

/* negative control: with no ReplayGuard configured the source defaults to an in-process guard, so a captured code still cannot be replayed out of the box. */
func TestTotpSecondFactor_ReplayedCodeIsRejectedByDefaultGuard(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})

    authenticator := totpAuthenticator(secret, true, nil)

    first, _ := authenticator.Authenticate(totpRequest(code))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of a valid code to authenticate")
    }

    second, _ := authenticator.Authenticate(totpRequest(code))
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
    _ runtimecontract.Runtime,
    _ string,
) (string, bool, error) {
    return instance.secret, instance.enrolled, nil
}

func (instance *recoveryEnrollmentStore) RedeemRecoveryCode(
    _ runtimecontract.Runtime,
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
    return NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
        Enrollments: store,
    })
}

func TestTotpSecondFactor_RecoveryCodeAuthenticatesAndIsSingleUse(t *testing.T) {
    store := &recoveryEnrollmentStore{enrolled: true, unused: map[string]bool{"abcde-fghij": true}}
    authenticator := totpAuthenticatorFor(store)

    first, err := authenticator.Authenticate(totpRecoveryRequest("abcde-fghij"))
    if nil != err {
        t.Fatalf("authenticate: %v", err)
    }

    if false == first.IsAuthenticated() || "user-1" != first.UserIdentifier() {
        t.Fatal("expected a valid recovery code to complete authentication")
    }

    second, _ := authenticator.Authenticate(totpRecoveryRequest("abcde-fghij"))
    if true == second.IsAuthenticated() {
        t.Fatal("expected a consumed recovery code to be rejected on reuse")
    }
}

/* negative control: a recovery code that is not one of the user's unused codes keeps the request pending. */
func TestTotpSecondFactor_UnknownRecoveryCodeIsPending(t *testing.T) {
    store := &recoveryEnrollmentStore{enrolled: true, unused: map[string]bool{"abcde-fghij": true}}

    token, _ := totpAuthenticatorFor(store).Authenticate(totpRecoveryRequest("zzzzz-zzzzz"))

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

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(totpRecoveryRequest("abcde-fghij"))

    if true == token.IsAuthenticated() {
        t.Fatal("expected recovery to be unavailable when the store does not support it")
    }

    if true == secondFactorRejected(t, token) {
        t.Fatal("expected the plain challenge, not the rejected form, when the store cannot read a recovery code")
    }
}

func TestTotpSecondFactor_ReplayWindowCoversEverySpanACodeVerifies(t *testing.T) {
    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
        Enrollments: &fixedEnrollmentStore{enrolled: false},
        Totp:        totp.Config{Period: bound.Of(60 * time.Second), Skew: bound.Of(10)},
    })

    if expected := 21 * 60 * time.Second; expected != authenticator.codeValidityWindow {
        t.Fatalf("expected the window to cover (2*10+1) periods (%v), got %v", expected, authenticator.codeValidityWindow)
    }
}

func TestTotpSecondFactor_AnonymousPrimaryPassesThrough(t *testing.T) {
    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAnonymousToken()},
        Enrollments: &fixedEnrollmentStore{enrolled: true},
    })

    token, _ := authenticator.Authenticate(totpRequest(""))
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
    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
        Enrollments: &fixedEnrollmentStore{secret: secret, enrolled: true},
        Clock:       frozen,
    })

    code, codeErr := totp.GenerateCodeAt(secret, frozen.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("code: %v", codeErr)
    }

    token, authenticateErr := authenticator.Authenticate(totpRequest(code))
    if nil != authenticateErr {
        t.Fatalf("authenticate: %v", authenticateErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatal("a code minted for the injected clock's instant was refused, so the authenticator read some other clock")
    }

    frozen.Advance(10 * time.Minute)

    lateToken, lateErr := authenticator.Authenticate(totpRequest(code))
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
    _ runtimecontract.Runtime,
    _ string,
) (string, bool, error) {
    return "", false, instance.lookupErr
}

func TestTotpSecondFactor_AFailingEnrollmentLookupRefusesInsteadOfPassingThrough(t *testing.T) {
    lookupErr := errors.New("enrollment store unavailable")

    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
        Enrollments: &failingEnrollmentStore{lookupErr: lookupErr},
        ReplayGuard: nil,
    })

    token, err := authenticator.Authenticate(totpRequest(""))
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
    runtimeInstance runtimecontract.Runtime,
    userIdentifier string,
) (string, bool, error) {
    return "JBSWY3DPEHPK3PXP", true, nil
}

func (instance *failingRecoveryEnrollmentStore) RedeemRecoveryCode(
    runtimeInstance runtimecontract.Runtime,
    userIdentifier string,
    recoveryCode string,
) (bool, error) {
    return false, instance.redeemErr
}

func TestTotpSecondFactor_MarksItsThreeStoreFailures(t *testing.T) {
    storeErr := errors.New("redis is down")
    primary := &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)}

    validCode, codeErr := totp.GenerateCodeAt("JBSWY3DPEHPK3PXP", time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("unexpected error: %v", codeErr)
    }

    for name, turn := range map[string]struct {
        authenticator *TotpSecondFactorAuthenticator
        request       httpcontract.Request
    }{
        "the enrollment lookup": {
            authenticator: NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{Primary: primary, Enrollments: &failingEnrollmentStore{lookupErr: storeErr}}),
            request:       totpRequest(""),
        },
        "the recovery redemption": {
            authenticator: NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{Primary: primary, Enrollments: &failingRecoveryEnrollmentStore{redeemErr: storeErr}}),
            request:       totpRecoveryRequest("recovery-1"),
        },
        "the replay guard": {
            authenticator: totpAuthenticator("JBSWY3DPEHPK3PXP", true, &failingNonceGuard{failure: storeErr}),
            request:       totpRequest(validCode),
        },
    } {
        t.Run(name, func(t *testing.T) {
            token, err := turn.authenticator.Authenticate(turn.request)
            if nil != token || false == errors.Is(err, storeErr) || false == exception.IsInfrastructureFailure(err) {
                t.Fatalf("expected the store failure marked, got %v, %v", token, err)
            }
        })
    }

    wrongCode, wrongErr := totpAuthenticator("JBSWY3DPEHPK3PXP", true, &failingNonceGuard{failure: storeErr}).Authenticate(totpRequest("000000"))
    if nil != wrongErr || true == wrongCode.IsAuthenticated() || false == secondFactorRejected(t, wrongCode) {
        t.Fatalf("expected a wrong code to stay a rejected token with no error, got %v, %v", wrongCode, wrongErr)
    }
}

func TestTotpSecondFactor_ASecretThatCannotBeReadIsNoOutage(t *testing.T) {
    token, err := totpAuthenticator("not base32!", true, nil).Authenticate(totpRequest("123456"))
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

func (instance *failingPrimaryAuthenticator) Authenticate(_ httpcontract.Request) (securitycontract.Token, error) {
    return nil, instance.err
}

func TestTotpSecondFactor_ThePrimarysErrorPassesThroughUnmarked(t *testing.T) {
    primaryErr := errors.New("malformed credentials")

    _, err := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &failingPrimaryAuthenticator{err: primaryErr},
        Enrollments: &fixedEnrollmentStore{secret: "JBSWY3DPEHPK3PXP", enrolled: true},
    }).Authenticate(totpRequest(""))
    if false == errors.Is(err, primaryErr) || true == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected the primary's error untouched, got %v", err)
    }
}

func TestTotpSecondFactor_AStoreFailureItsStoreFiledStaysFiled(t *testing.T) {
    filed := exception.NewError("enrollment store unavailable", nil, nil)
    _ = exception.MarkLogged(filed)

    _, filedErr := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
        Enrollments: &failingEnrollmentStore{lookupErr: filed},
    }).Authenticate(totpRequest(""))
    if false == exception.IsAlreadyLogged(filedErr) || false == exception.IsInfrastructureFailure(filedErr) {
        t.Fatalf("expected a filed store failure to stay filed under the mark, got %v", filedErr)
    }

    _, unfiledErr := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
        Enrollments: &failingEnrollmentStore{lookupErr: errors.New("enrollment store unavailable")},
    }).Authenticate(totpRequest(""))
    if true == exception.IsAlreadyLogged(unfiledErr) {
        t.Fatalf("expected an unfiled store failure left to be filed, got %v", unfiledErr)
    }
}

func TestTotpSecondFactor_APrimaryThatCouldNotCheckTheCredentialIsPassedThrough(t *testing.T) {
    unavailable := &unavailableAuthenticatedToken{}

    token, err := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: unavailable},
        Enrollments: &fixedEnrollmentStore{secret: "JBSWY3DPEHPK3PXP", enrolled: true},
    }).Authenticate(totpRequest(""))
    if nil != err || securitycontract.Token(unavailable) != token {
        t.Fatalf("expected the unavailable token passed through, got %T, %v", token, err)
    }
}
