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
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
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
        _ = NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: ""})
    }, "hmac signer application is empty")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(context.Background(), nil, HmacEnvelopeSignerConfig{Application: "billing"})
    }, "hmac signer secrets provider is nil")

    var typedNilSecrets HmacSecretProvider = (*StaticHmacSecretProvider)(nil)
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(context.Background(), typedNilSecrets, HmacEnvelopeSignerConfig{Application: "billing"})
    }, "hmac signer secrets provider is nil")
}

/* the verifier refuses an envelope whose key id is not bound to its claimed application, so a signer whose current key belongs to someone else would emit envelopes that are silently rejected on the other side, one request at a time, with nothing on this end to say why */
func TestNewHmacEnvelopeSigner_RefusesACurrentKeyIssuedToAnotherApplication(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "crm"})
    }, "hmac signer current key id is not bound to the signer application")

    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || "crm" != refusal.Context()["signerApplication"] || "billing" != refusal.Context()["boundApplication"] {
            t.Fatalf("expected the refusal to name both applications, got %v", refusal)
        }
    }()

    _ = NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "crm"})
}

func TestNewHmacEnvelopeSigner_FallsBackToTheDefaultHeaderName(t *testing.T) {
    signer := NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "billing"})

    if DefaultHmacHeaderName != signer.HeaderName() {
        t.Fatalf("expected the default header name, got %q", signer.HeaderName())
    }

    named := NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "billing", HeaderName: "X-Own-Auth"})
    if "X-Own-Auth" != named.HeaderName() {
        t.Fatalf("expected the configured header name, got %q", named.HeaderName())
    }
}

func TestHmacEnvelopeSigner_AnAbsentTtlSignsForThirtySeconds(t *testing.T) {
    signer := NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{
        Application: "billing",
        Clock:       clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/orders", nil, nil)
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

func TestHmacEnvelopeSigner_RefusesATtlUnderASecondAndUnlimited(t *testing.T) {
    for _, ttl := range []bound.Duration{bound.Of(500 * time.Millisecond), bound.Of(-time.Minute), bound.UnlimitedDuration()} {
        expectPanicNamingField(t, "HmacEnvelopeSignerConfig.Ttl", func() {
            NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "billing", Ttl: ttl})
        })
    }
}

/* the path argument may arrive with a query string attached; the envelope signs the two separately because the callee matches them against the request's path and its raw query, which are separate there too */
func TestHmacEnvelopeSigner_SignSplitsTheQueryOffThePath(t *testing.T) {
    signer := NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{
        Application: "billing",
        Audience:    "warehouse",
        Clock:       clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    headerValue, signErr := signer.Sign(newTestRuntime(), "POST", "/orders?page=2&size=10", []byte("payload"), nil)
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
    signer := NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{
        Application: "billing",
        Clock:       clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    firstValue, _ := signer.Sign(newTestRuntime(), "GET", "/orders", nil, nil)
    secondValue, _ := signer.Sign(newTestRuntime(), "GET", "/orders", nil, nil)

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
        signer := NewHmacEnvelopeSigner(context.Background(), provider, HmacEnvelopeSignerConfig{Application: "wms-service"})
        fail(provider)

        headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, nil)
        if "" != headerValue || false == errors.Is(signErr, failure) || false == exception.AnswersUnavailable(signErr) {
            t.Fatalf("expected the %s failure returned marked, got %q %v", door, headerValue, signErr)
        }
    }
}

func TestHmacEnvelopeSigner_SignRefusesANilRuntime(t *testing.T) {
    signer := NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "billing"})

    if _, signErr := signer.Sign(nil, "GET", "/internal", nil, nil); "runtimeInstance" != exception.LogContext(signErr, exception.LogContextConfig{})["argument"] {
        t.Fatalf("expected a nil runtime refused naming the argument, got %v", signErr)
    }
}

func TestHmacEnvelopeSigner_SignRefusesACurrentKeyMovedToAnotherApplication(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    signer := NewHmacEnvelopeSigner(context.Background(), provider, HmacEnvelopeSignerConfig{Application: "wms-service"})

    provider.keyId = "key-rotated"
    provider.application = "crm"

    signErr := func() error {
        _, err := signer.Sign(newTestRuntime(), "GET", "/internal", nil, nil)

        return err
    }()
    if nil == signErr || false == strings.Contains(signErr.Error(), "not bound to the signer application") || true == exception.IsInfrastructureFailure(signErr) {
        t.Fatalf("expected a rotated key bound elsewhere refused unmarked, got %v", signErr)
    }
}

func TestHmacEnvelopeSigner_SignJudgesTheBindingOnlyWhenTheCurrentKeyIdChanges(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    signer := NewHmacEnvelopeSigner(context.Background(), provider, HmacEnvelopeSignerConfig{Application: "wms-service"})

    provider.application = "crm"
    if _, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, nil); nil != signErr {
        t.Fatalf("expected the key judged at construction not judged again, got %v", signErr)
    }

    provider.keyId = "key-rotated"
    if _, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, nil); nil == signErr {
        t.Fatalf("expected the rotated key judged and refused")
    }

    provider.application = ""
    if _, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, nil); nil != signErr {
        t.Fatalf("expected the rotated key signing once bound, got %v", signErr)
    }
}

func TestHmacEnvelopeSigner_AnyProvidersUnboundKeyIsRefusedAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(context.Background(), &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret), application: "crm"}, HmacEnvelopeSignerConfig{Application: "wms-service"})
    }, "hmac signer current key id is not bound to the signer application")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(context.Background(), hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "crm"})
    }, "hmac signer current key id is not bound to the signer application")
}

func TestHmacEnvelopeSigner_AProviderThatCannotAnswerPanicsAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(context.Background(), &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret), secretErr: errors.New("down")}, HmacEnvelopeSignerConfig{Application: "wms-service"})
    }, "hmac signer secret provider failed")

}

func TestHmacEnvelopeSigner_ANilContextPanicsAtConstruction(t *testing.T) {
    var typedNilContext *nilContextProbe

    for name, ctx := range map[string]context.Context{"nil": nil, "typed nil": typedNilContext} {
        t.Run(name, func(t *testing.T) {
            testhelper.AssertPanicsWithError(t, func() {
                _ = NewHmacEnvelopeSigner(ctx, hmacSignerProbeSecrets(), HmacEnvelopeSignerConfig{Application: "billing"})
            }, "hmac signer context is nil")
        })
    }
}

type nilContextProbe struct {
    context.Context
}

type hmacSignerContextKey struct{}

func TestHmacEnvelopeSigner_AsksTheProviderUnderTheConstructionContextThenTheRuntimesContext(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    bootContext := context.WithValue(context.Background(), hmacSignerContextKey{}, "boot")

    signer := NewHmacEnvelopeSigner(bootContext, provider, HmacEnvelopeSignerConfig{Application: "wms-service"})

    constructionContexts := provider.contextsSeen()
    if 3 != len(constructionContexts) {
        t.Fatalf("expected the current key, its binding and its secret read at construction, got %d reads", len(constructionContexts))
    }

    for _, seen := range constructionContexts {
        if bootContext != seen {
            t.Fatalf("expected every construction read under the context given to the constructor")
        }
    }

    signContainer := container.NewContainer(container.ContainerConfig{})
    signRuntime := runtime.NewRequest(context.WithValue(context.Background(), hmacSignerContextKey{}, "sign"), signContainer.NewScope())
    if _, signErr := signer.Sign(signRuntime, "GET", "/internal", nil, nil); nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    signContexts := provider.contextsSeen()[len(constructionContexts):]
    if 0 == len(signContexts) {
        t.Fatalf("expected Sign to read the provider")
    }

    for _, seen := range signContexts {
        if signRuntime.Context() != seen {
            t.Fatalf("expected every Sign read under the runtime's context")
        }
    }
}

func TestHmacEnvelopeSigner_ACurrentKeyWithoutASecretIsRefusedAtConstruction(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(context.Background(), &missingSecretHmacProvider{}, HmacEnvelopeSignerConfig{Application: "wms-service"})
    }, "hmac signer has no secret for the current key id")
}

type missingSecretHmacProvider struct {
    customHmacSecretProvider
}

func (instance *missingSecretHmacProvider) Secret(_ context.Context, _ string) ([]byte, bool, error) {
    return nil, false, nil
}
