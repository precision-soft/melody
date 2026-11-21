package security

import (
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
)

type runtimeRecordingAuthenticator struct {
    runtimes []containercontract.Runtime
}

func (instance *runtimeRecordingAuthenticator) Supports(request httpcontract.Request) bool {
    return true
}

func (instance *runtimeRecordingAuthenticator) Authenticate(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil), nil
}

func TestAuthenticatorManager_HandsItsRuntimeToTheAuthenticator(t *testing.T) {
    authenticator := &runtimeRecordingAuthenticator{}
    runtimeInstance := newTestRuntime()

    if _, _, err := NewAuthenticatorManager([]securitycontract.Authenticator{authenticator}).Authenticate(runtimeInstance, newFirewallTestRequest("/x")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != len(authenticator.runtimes) || runtimeInstance != authenticator.runtimes[0] {
        t.Fatalf("expected the authenticator handed the manager's runtime, got %v", authenticator.runtimes)
    }
}

type runtimeRecordingRule struct {
    runtimes []containercontract.Runtime
}

func (instance *runtimeRecordingRule) Applies(request httpcontract.Request) bool {
    return true
}

func (instance *runtimeRecordingRule) Check(runtimeInstance containercontract.Runtime, request httpcontract.Request) error {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return nil
}

func TestFirewall_Check_HandsItsRuntimeToTheRule(t *testing.T) {
    rule := &runtimeRecordingRule{}
    runtimeInstance := newTestRuntime()

    if err := NewFirewall([]securitycontract.Rule{rule}).Check(runtimeInstance, newFirewallTestRequest("/x")); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != len(rule.runtimes) || runtimeInstance != rule.runtimes[0] {
        t.Fatalf("expected the rule handed the firewall's runtime, got %v", rule.runtimes)
    }
}

type runtimeRecordingEnrollmentStore struct {
    secret   string
    runtimes []containercontract.Runtime
}

func (instance *runtimeRecordingEnrollmentStore) FindTotpSecret(runtimeInstance containercontract.Runtime, userIdentifier string) (string, bool, error) {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return instance.secret, true, nil
}

type runtimeRecordingNonceGuard struct {
    runtimes []containercontract.Runtime
}

func (instance *runtimeRecordingNonceGuard) Remember(runtimeInstance containercontract.Runtime, nonce string, ttl bound.Duration) (bool, error) {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return false, nil
}

func TestTotpSecondFactorAuthenticator_ReadsItsStoresThroughTheRuntimeItWasHanded(t *testing.T) {
    secret := "JBSWY3DPEHPK3PXP"
    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("generate: %v", codeErr)
    }

    store := &runtimeRecordingEnrollmentStore{secret: secret}
    guard := &runtimeRecordingNonceGuard{}
    primary := &runtimeRecordingAuthenticator{}
    authenticator := NewTotpSecondFactorAuthenticator(TotpSecondFactorAuthenticatorConfig{Primary: primary, Enrollments: store, ReplayGuard: guard})

    runtimeInstance := newTestRuntime()
    token, err := authenticator.Authenticate(runtimeInstance, totpRequest(code))
    if nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the second factor accepted, got %v, %v", token, err)
    }

    for name, runtimes := range map[string][]containercontract.Runtime{"primary": primary.runtimes, "enrollments": store.runtimes, "replay guard": guard.runtimes} {
        if 1 != len(runtimes) || runtimeInstance != runtimes[0] {
            t.Fatalf("expected the %s handed the runtime the authenticator was handed, on a request that carries none, got %v", name, runtimes)
        }
    }
}

func TestResolverTokenSource_ResolveHandsTheRuntimeToTheResolver(t *testing.T) {
    runtimeInstance := newTestRuntime()
    var seen containercontract.Runtime

    source := NewResolverTokenSource(func(resolverRuntime containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
        seen = resolverRuntime

        return nil, nil
    }, ResolverTokenSourceConfig{})

    if _, err := source.Resolve(runtimeInstance, newFirewallTestRequest("/x")); nil != err || runtimeInstance != seen {
        t.Fatalf("expected the resolver handed the source's runtime, got %v (%v)", seen, err)
    }
}

func TestResolverTokenSource_ResolveAnswersTheResolversErrorAndNoToken(t *testing.T) {
    resolveErr := errors.New("the session store is down")
    source := NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
        return NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil), resolveErr
    }, ResolverTokenSourceConfig{})

    token, err := source.Resolve(newTestRuntime(), newFirewallTestRequest("/x"))
    if nil != token || false == errors.Is(err, resolveErr) {
        t.Fatalf("expected no token beside the resolver's error, got %v, %v", token, err)
    }
}

type typedNilResolverError struct{}

func (instance *typedNilResolverError) Error() string { return "never rendered" }

func TestResolverTokenSource_ATypedNilResolverErrorIsNoError(t *testing.T) {
    var typedNil *typedNilResolverError
    source := NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
        return NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil), typedNil
    }, ResolverTokenSourceConfig{})

    token, err := source.Resolve(newTestRuntime(), newFirewallTestRequest("/x"))
    if nil != err || nil == token || false == token.IsAuthenticated() {
        t.Fatalf("expected the token answered beside a typed nil error, got %v, %v", token, err)
    }
}


func TestAuthenticatorTokenSource_ResolveHandsItsRuntimeToTheAuthenticator(t *testing.T) {
    authenticator := &runtimeRecordingAuthenticator{}
    runtimeInstance, _ := newTokenSourceTestRuntime(t)

    source := NewAuthenticatorTokenSource(NewAuthenticatorManager([]securitycontract.Authenticator{authenticator}), AuthenticatorTokenSourceConfig{})
    if _, err := source.Resolve(runtimeInstance, newSecurityTestRequest("GET", "/x", nil, newTestRuntime())); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != len(authenticator.runtimes) || runtimeInstance != authenticator.runtimes[0] {
        t.Fatalf("expected the authenticator handed the source's runtime, not the request's, got %v", authenticator.runtimes)
    }
}

type runtimeRecordingRecoveryStore struct {
    runtimes []containercontract.Runtime
}

func (instance *runtimeRecordingRecoveryStore) FindTotpSecret(runtimeInstance containercontract.Runtime, userIdentifier string) (string, bool, error) {
    return "JBSWY3DPEHPK3PXP", true, nil
}

func (instance *runtimeRecordingRecoveryStore) RedeemRecoveryCode(runtimeInstance containercontract.Runtime, userIdentifier string, code string) (bool, error) {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return true, nil
}

func TestTotpSecondFactorAuthenticator_RedeemsARecoveryCodeThroughTheRuntimeItWasHanded(t *testing.T) {
    store := &runtimeRecordingRecoveryStore{}
    authenticator := totpAuthenticatorFor(store)

    runtimeInstance := newTestRuntime()
    token, err := authenticator.Authenticate(runtimeInstance, totpRecoveryRequest("abcde-fghij"))
    if nil != err || false == token.IsAuthenticated() {
        t.Fatalf("expected the recovery code accepted, got %v, %v", token, err)
    }

    if 1 != len(store.runtimes) || runtimeInstance != store.runtimes[0] {
        t.Fatalf("expected the recovery store handed the authenticator's runtime, got %v", store.runtimes)
    }
}
