package security

import (
    "errors"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/security/totp"
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
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
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
}

func TestTotpSecondFactor_ReplayWindowStaysPositiveOnOverflow(t *testing.T) {
    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
        Enrollments: &fixedEnrollmentStore{enrolled: false},
        Totp:        totp.Config{Period: 3333333334, Skew: 1},
    })

    if window := authenticator.codeValidityWindow(); 0 >= window {
        t.Fatalf("expected a strictly positive replay window, got %v", window)
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

func TestTotpSecondFactor_WrongCodeIsPending(t *testing.T) {
    secret, _ := totp.GenerateSecret()

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(totpRequest("000000"))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a wrong code to stay pending")
    }
}

func TestTotpSecondFactor_ReplayedCodeIsRejected(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})

    authenticator := totpAuthenticator(secret, true, NewMemoryNonceGuard())

    first, _ := authenticator.Authenticate(totpRequest(code))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of a valid code to authenticate")
    }

    second, _ := authenticator.Authenticate(totpRequest(code))
    if true == second.IsAuthenticated() {
        t.Fatal("expected a replayed code to be rejected by the guard")
    }
}

func TestTotpSecondFactor_ReplayedCodeIsRejectedWhenRespaced(t *testing.T) {
    secret, _ := totp.GenerateSecret()
    code, _ := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})

    authenticator := totpAuthenticator(secret, true, NewMemoryNonceGuard())

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
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
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

func TestTotpSecondFactor_UnknownRecoveryCodeIsPending(t *testing.T) {
    store := &recoveryEnrollmentStore{enrolled: true, unused: map[string]bool{"abcde-fghij": true}}

    token, _ := totpAuthenticatorFor(store).Authenticate(totpRecoveryRequest("zzzzz-zzzzz"))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an unknown recovery code to stay pending")
    }
}

func TestTotpSecondFactor_RecoveryIgnoredWhenStoreUnsupported(t *testing.T) {
    secret, _ := totp.GenerateSecret()

    token, _ := totpAuthenticator(secret, true, nil).Authenticate(totpRecoveryRequest("abcde-fghij"))

    if true == token.IsAuthenticated() {
        t.Fatal("expected recovery to be unavailable when the store does not support it")
    }
}

func TestTotpSecondFactor_ReplayWindowMirrorsClampedSkew(t *testing.T) {
    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
        Enrollments: &fixedEnrollmentStore{enrolled: false},
        Totp:        totp.Config{Period: 30, Skew: 1_000_000},
    })

    expected := time.Duration(21*30) * time.Second
    if window := authenticator.codeValidityWindow(); expected != window {
        t.Fatalf("expected the window to mirror the maxSkew-clamped skew (%v), got %v", expected, window)
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

func TestTotpSecondFactor_VerifiesOnTheInjectedClock(t *testing.T) {
    secret, secretErr := totp.GenerateSecret()
    if nil != secretErr {
        t.Fatalf("secret: %v", secretErr)
    }

    frozen := clock.NewFrozenClock(time.Unix(1_000_000, 0))
    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
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
        Primary:     &fixedAuthenticator{token: NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
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
