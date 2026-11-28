package security

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func hmacSignerProbeSecrets() *StaticHmacSecretProvider {
    return NewStaticHmacSecretProvider(HmacKeyRing{
        CurrentKeyId: "key-current",
        Keys: map[string]HmacKey{
            "key-current": {Application: "billing", Secret: []byte("current-shared-secret-value-0001")},
        },
    })
}

func TestNewHmacEnvelopeSigner_RefusesASignerThatCanNotIdentifyItself(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "", HmacEnvelopeSignerConfig{})
    }, "hmac signer application is empty")

    func() {
        defer func() {
            refusal, isRefusal := recover().(*exception.Error)
            if false == isRefusal || "application" != refusal.Context()["argument"] {
                t.Fatalf("expected the empty application refused naming the argument, got %v", refusal)
            }
        }()

        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "", HmacEnvelopeSignerConfig{})
    }()

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), nil, "billing", HmacEnvelopeSignerConfig{})
    }, "hmac signer secrets provider is nil")

    var typedNilSecrets HmacSecretProvider = (*StaticHmacSecretProvider)(nil)
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), typedNilSecrets, "billing", HmacEnvelopeSignerConfig{})
    }, "hmac signer secrets provider is nil")
}

/* the verifier refuses an envelope whose key id is not bound to its claimed application, so a signer whose current key belongs to someone else would emit envelopes that are silently rejected on the other side, one request at a time, with nothing on this end to say why */
func TestNewHmacEnvelopeSigner_RefusesACurrentKeyIssuedToAnotherApplication(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "crm", HmacEnvelopeSignerConfig{})
    }, "hmac signer current key id is not bound to the signer application")

    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || "crm" != refusal.Context()["signerApplication"] || "billing" != refusal.Context()["boundApplication"] {
            t.Fatalf("expected the refusal to name both applications, got %v", refusal)
        }
    }()

    _ = NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "crm", HmacEnvelopeSignerConfig{})
}

func TestNewHmacEnvelopeSigner_FallsBackToTheDefaultHeaderName(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{})

    if DefaultHmacHeaderName != signer.HeaderName() {
        t.Fatalf("expected the default header name, got %q", signer.HeaderName())
    }

    named := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{HeaderName: "X-Own-Auth"})
    if "X-Own-Auth" != named.HeaderName() {
        t.Fatalf("expected the configured header name, got %q", named.HeaderName())
    }
}

func TestHmacEnvelopeSigner_AnAbsentTtlSignsForThirtySeconds(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{
        Clock: clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/orders", nil, SignOptions{})
    if nil != signErr {
        t.Fatalf("unexpected sign error: %v", signErr)
    }

    envelope, _, decodeErr := decodeHmacHeaderValue(newTestRuntime(), headerValue, hmacSignerProbeSecrets(), minimumHmacSha256SecretLength)
    if nil != decodeErr {
        t.Fatalf("unexpected decode error: %v", decodeErr)
    }

    if 1030 != envelope.ExpiresAt {
        t.Fatalf("expected the envelope to expire 30s after its issue, got exp=%d iat=%d", envelope.ExpiresAt, envelope.IssuedAt)
    }
}

func TestHmacEnvelopeSigner_SignPropagatesTheOptionsActor(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{})
    actor := NewActor("user-7", securitycontract.ActorTypeUser, ActorConfig{Roles: []string{"ROLE_BUYER"}})

    headerValue, signErr := signer.Sign(newTestRuntime(), "POST", "/orders", nil, SignOptions{Actor: actor})
    if nil != signErr {
        t.Fatalf("unexpected sign error: %v", signErr)
    }

    envelope, _, decodeErr := decodeHmacHeaderValue(newTestRuntime(), headerValue, hmacSignerProbeSecrets(), minimumHmacSha256SecretLength)
    if nil != decodeErr {
        t.Fatalf("unexpected decode error: %v", decodeErr)
    }

    if nil == envelope.Actor || "user-7" != envelope.Actor.Identifier || securitycontract.ActorTypeUser != envelope.Actor.Type {
        t.Fatalf("expected the envelope to carry the options' actor, got %+v", envelope.Actor)
    }
}

func TestHmacEnvelopeSigner_SignWithoutAnActorCarriesNone(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{})
    var typedNil *Actor

    for name, options := range map[string]SignOptions{"left out": {}, "typed nil": {Actor: typedNil}} {
        headerValue, signErr := signer.Sign(newTestRuntime(), "POST", "/orders", nil, options)
        if nil != signErr {
            t.Fatalf("%s: unexpected sign error: %v", name, signErr)
        }

        envelope, _, decodeErr := decodeHmacHeaderValue(newTestRuntime(), headerValue, hmacSignerProbeSecrets(), minimumHmacSha256SecretLength)
        if nil != decodeErr {
            t.Fatalf("%s: unexpected decode error: %v", name, decodeErr)
        }

        if nil != envelope.Actor {
            t.Fatalf("%s: expected no actor in the envelope, got %+v", name, envelope.Actor)
        }
    }
}

func TestHmacEnvelopeSigner_RefusesATtlUnderASecondAndUnlimited(t *testing.T) {
    for _, ttl := range []bound.Duration{bound.Of(500 * time.Millisecond), bound.Of(-time.Minute), bound.UnlimitedDuration()} {
        expectPanicNamingField(t, "HmacEnvelopeSignerConfig.Ttl", func() {
            NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{Ttl: ttl})
        })
    }
}

/* the path argument may arrive with a query string attached; the envelope signs the two separately because the callee matches them against the request's path and its raw query, which are separate there too */
func TestHmacEnvelopeSigner_SignSplitsTheQueryOffThePath(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{
        Audience: "warehouse",
        Clock:    clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    headerValue, signErr := signer.Sign(newTestRuntime(), "POST", "/orders?page=2&size=10", []byte("payload"), SignOptions{})
    if nil != signErr {
        t.Fatalf("unexpected sign error: %v", signErr)
    }

    envelope, keyId, decodeErr := decodeHmacHeaderValue(newTestRuntime(), headerValue, hmacSignerProbeSecrets(), minimumHmacSha256SecretLength)
    if nil != decodeErr {
        t.Fatalf("unexpected decode error: %v", decodeErr)
    }

    if "key-current" != keyId {
        t.Fatalf("expected the current key id to sign, got %q", keyId)
    }

    if "/orders" != envelope.Path {
        t.Fatalf("expected the query to be cut off the path, got %q", envelope.Path)
    }

    if "page=2&size=10" != envelope.Query {
        t.Fatalf("expected the query to be signed on its own, got %q", envelope.Query)
    }

    if "warehouse" != envelope.Audience {
        t.Fatalf("expected the configured audience to be signed in, got %q", envelope.Audience)
    }

    if hashBody([]byte("payload")) != envelope.BodyHash {
        t.Fatalf("expected the body hash to cover the body, got %q", envelope.BodyHash)
    }
}

/* the nonce is what makes an envelope single-use, so two envelopes signed by the same signer at the same frozen instant must still differ */
func TestHmacEnvelopeSigner_SignMintsAFreshNonceEachTime(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{
        Clock: clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    firstValue, _ := signer.Sign(newTestRuntime(), "GET", "/orders", nil, SignOptions{})
    secondValue, _ := signer.Sign(newTestRuntime(), "GET", "/orders", nil, SignOptions{})

    first, _, _ := decodeHmacHeaderValue(newTestRuntime(), firstValue, hmacSignerProbeSecrets(), minimumHmacSha256SecretLength)
    second, _, _ := decodeHmacHeaderValue(newTestRuntime(), secondValue, hmacSignerProbeSecrets(), minimumHmacSha256SecretLength)

    if "" == first.Nonce || first.Nonce == second.Nonce {
        t.Fatalf("expected a fresh nonce per envelope, got %q twice", first.Nonce)
    }

    if true == strings.Contains(first.Nonce, "=") {
        t.Fatalf("expected an unpadded base64url nonce, got %q", first.Nonce)
    }
}

func TestHmacEnvelopeSigner_SignReturnsAProviderFailureMarked(t *testing.T) {
    failure := errors.New("the secret manager is down")

    for door, fail := range map[string]func(provider *customHmacSecretProvider){
        "CurrentKeyId":        func(provider *customHmacSecretProvider) { provider.currentKeyIdErr = failure },
        "Secret":              func(provider *customHmacSecretProvider) { provider.secretErr = failure },
        "ApplicationForKeyId": func(provider *customHmacSecretProvider) { provider.applicationErr = failure; provider.keyId = "key-rotated" },
    } {
        provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
        signer := NewHmacEnvelopeSigner(hmacTestRuntime(), provider, "wms-service", HmacEnvelopeSignerConfig{})
        fail(provider)

        headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, SignOptions{})
        if "" != headerValue || false == errors.Is(signErr, failure) || false == exception.AnswersUnavailable(signErr) {
            t.Fatalf("expected the %s failure returned marked, got %q %v", door, headerValue, signErr)
        }
    }
}

func TestHmacEnvelopeSigner_SignRefusesANilRuntime(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{})

    if _, signErr := signer.Sign(nil, "GET", "/internal", nil, SignOptions{}); "runtimeInstance" != exception.LogContext(signErr, exception.LogContextOptions{})["argument"] {
        t.Fatalf("expected a nil runtime refused naming the argument, got %v", signErr)
    }
}

func TestHmacEnvelopeSigner_SignRefusesACurrentKeyMovedToAnotherApplication(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), provider, "wms-service", HmacEnvelopeSignerConfig{})

    provider.keyId = "key-rotated"
    provider.application = "crm"

    signErr := func() error {
        _, err := signer.Sign(newTestRuntime(), "GET", "/internal", nil, SignOptions{})

        return err
    }()
    if nil == signErr || false == strings.Contains(signErr.Error(), "not bound to the signer application") || true == exception.IsInfrastructureFailure(signErr) {
        t.Fatalf("expected a rotated key bound elsewhere refused unmarked, got %v", signErr)
    }
}

func TestHmacEnvelopeSigner_SignJudgesTheBindingOnlyWhenTheCurrentKeyIdChanges(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), provider, "wms-service", HmacEnvelopeSignerConfig{})

    provider.application = "crm"
    if _, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, SignOptions{}); nil != signErr {
        t.Fatalf("expected the key judged at construction not judged again, got %v", signErr)
    }

    provider.keyId = "key-rotated"
    if _, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, SignOptions{}); nil == signErr {
        t.Fatalf("expected the rotated key judged and refused")
    }

    provider.application = ""
    if _, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, SignOptions{}); nil != signErr {
        t.Fatalf("expected the rotated key signing once bound, got %v", signErr)
    }
}

func TestHmacEnvelopeSigner_AnyProvidersUnboundKeyIsRefusedAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret), application: "crm"}, "wms-service", HmacEnvelopeSignerConfig{})
    }, "hmac signer current key id is not bound to the signer application")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), hmacSignerProbeSecrets(), "crm", HmacEnvelopeSignerConfig{})
    }, "hmac signer current key id is not bound to the signer application")
}

func TestHmacEnvelopeSigner_AProviderThatCannotAnswerPanicsAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret), secretErr: errors.New("down")}, "wms-service", HmacEnvelopeSignerConfig{})
    }, "hmac signer secret provider failed")

}

type typedNilSignerRuntime struct {
    containercontract.Runtime
}

func TestHmacEnvelopeSigner_ANilRuntimePanicsAtConstruction(t *testing.T) {
    var typedNil *typedNilSignerRuntime

    for name, runtimeInstance := range map[string]containercontract.Runtime{"nil": nil, "typed nil": typedNil} {
        t.Run(name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, func() {
                _ = NewHmacEnvelopeSigner(runtimeInstance, hmacSignerProbeSecrets(), "billing", HmacEnvelopeSignerConfig{})
            }, "the runtime is nil or carries no context")
        })
    }
}

type hmacSignerContextKey struct{}

func TestHmacEnvelopeSigner_AsksTheProviderUnderTheConstructionRuntimeThenTheCallersRuntimeByIdentity(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    bootRuntime, finish := runtime.NewBoot(context.WithValue(context.Background(), hmacSignerContextKey{}, "boot"), container.NewContainer(container.ContainerConfig{}))
    defer func() {
        _ = finish()
    }()

    signer := NewHmacEnvelopeSigner(bootRuntime, provider, "wms-service", HmacEnvelopeSignerConfig{})

    constructionRuntimes := provider.runtimesSeen()
    if 3 != len(constructionRuntimes) {
        t.Fatalf("expected the current key, its binding and its secret read at construction, got %d reads", len(constructionRuntimes))
    }

    for _, seen := range constructionRuntimes {
        if bootRuntime != seen || containercontract.KindBoot != seen.Kind() {
            t.Fatalf("expected every construction read under the boot runtime given to the constructor")
        }
    }

    signContainer := container.NewContainer(container.ContainerConfig{})
    signRuntime := runtime.NewRequest(context.WithValue(context.Background(), hmacSignerContextKey{}, "sign"), signContainer.NewScope())
    if _, signErr := signer.Sign(signRuntime, "GET", "/internal", nil, SignOptions{}); nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    signRuntimes := provider.runtimesSeen()[len(constructionRuntimes):]
    if 0 == len(signRuntimes) {
        t.Fatalf("expected Sign to read the provider")
    }

    for _, seen := range signRuntimes {
        if signRuntime != seen {
            t.Fatalf("expected every Sign read under the caller's runtime, by identity")
        }
    }
}

func TestHmacEnvelopeSigner_ACurrentKeyWithoutASecretIsRefusedAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(hmacTestRuntime(), &missingSecretHmacProvider{}, "wms-service", HmacEnvelopeSignerConfig{})
    }, "hmac signer has no secret for the current key id")
}

type missingSecretHmacProvider struct {
    customHmacSecretProvider
}

func (instance *missingSecretHmacProvider) Secret(_ containercontract.Runtime, _ string) ([]byte, bool, error) {
    return nil, false, nil
}
