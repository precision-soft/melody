package security

import (
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
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
