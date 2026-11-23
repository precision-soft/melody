package security

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

const thirtyTwoByteSecret = "a-secret-of-exactly-32-bytes-ok!"

type customHmacSecretProvider struct {
    secret          []byte
    keyId           string
    application     string
    currentKeyIdErr error
    secretErr       error
    applicationErr  error
    mutex           sync.Mutex
    seenRuntimes    []containercontract.Runtime
}

func (instance *customHmacSecretProvider) record(runtimeInstance containercontract.Runtime) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.seenRuntimes = append(instance.seenRuntimes, runtimeInstance)
}

func (instance *customHmacSecretProvider) runtimesSeen() []containercontract.Runtime {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]containercontract.Runtime{}, instance.seenRuntimes...)
}

func (instance *customHmacSecretProvider) currentKeyIdValue() string {
    if "" == instance.keyId {
        return "key-custom"
    }

    return instance.keyId
}

func (instance *customHmacSecretProvider) CurrentKeyId(runtimeInstance containercontract.Runtime) (string, error) {
    instance.record(runtimeInstance)

    if nil != instance.currentKeyIdErr {
        return "", instance.currentKeyIdErr
    }

    return instance.currentKeyIdValue(), nil
}

func (instance *customHmacSecretProvider) Secret(runtimeInstance containercontract.Runtime, keyId string) ([]byte, bool, error) {
    instance.record(runtimeInstance)

    if nil != instance.secretErr {
        return nil, false, instance.secretErr
    }

    if instance.currentKeyIdValue() != keyId {
        return nil, false, nil
    }

    return append([]byte{}, instance.secret...), true, nil
}

func (instance *customHmacSecretProvider) ApplicationForKeyId(runtimeInstance containercontract.Runtime, keyId string) (string, bool, error) {
    instance.record(runtimeInstance)

    if nil != instance.applicationErr {
        return "", false, instance.applicationErr
    }

    if instance.currentKeyIdValue() != keyId {
        return "", false, nil
    }

    if "" != instance.application {
        return instance.application, true, nil
    }

    return "wms-service", true, nil
}

func expectShortSecretRefusal(t *testing.T, key string, field string, secretLength int, minimumLength int, keyId string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected a refusal naming %s", field)
        }

        context := refusal.Context()
        if field != context[key] || secretLength != context["secretLength"] || minimumLength != context["minimumLength"] {
            t.Fatalf("expected %s refused at %d under %d, got %q %v", field, secretLength, minimumLength, refusal.Message(), context)
        }

        if "" != keyId && keyId != context["keyId"] {
            t.Fatalf("expected the key id %q named, got %v", keyId, context)
        }

        for _, value := range context {
            if rendered, isString := value.(string); true == isString && true == strings.Contains(rendered, "short") {
                t.Fatalf("expected the secret never named, got %v", context)
            }
        }
    }()

    callback()
}

func TestNewStaticHmacSecretProvider_RefusesAShortSecretOfAnyKey(t *testing.T) {
    expectShortSecretRefusal(t, "field", "HmacKeyRing.Keys", 9, 32, "key-previous", func() {
        NewStaticHmacSecretProvider(HmacKeyRing{
            CurrentKeyId: "key-current",
            Keys: map[string]HmacKey{
                "key-current":  {Application: "wms-service", Secret: []byte("current-shared-secret-value-0001")},
                "key-previous": {Application: "wms-service", Secret: []byte("short-key")},
            },
        })
    })

    expectShortSecretRefusal(t, "field", "HmacKeyRing.Keys", 32, 48, "key-current", func() {
        NewStaticHmacSecretProvider(HmacKeyRing{
            CurrentKeyId:        "key-current",
            Keys:                map[string]HmacKey{"key-current": {Application: "wms-service", Secret: []byte(thirtyTwoByteSecret)}},
            MinimumSecretLength: bound.Of(48),
        })
    })

    expectPanicNamingField(t, "HmacKeyRing.MinimumSecretLength", func() {
        NewStaticHmacSecretProvider(HmacKeyRing{
            CurrentKeyId:        "key-current",
            Keys:                map[string]HmacKey{"key-current": {Application: "wms-service", Secret: []byte(thirtyTwoByteSecret)}},
            MinimumSecretLength: bound.Of(16),
        })
    })
}

func TestHmacTokenSource_AShortSecretOfACustomProviderVerifiesNothing(t *testing.T) {
    now := time.Now()
    envelope := hmacEnvelope{Application: "wms-service", Method: "GET", Path: "/internal", IssuedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(), Nonce: "nonce-short", BodyHash: hashBody(nil)}

    shortSecret := []byte("short-custom-secret")
    headerValue, encodeErr := encodeHmacHeaderValue("key-custom", envelope, shortSecret)
    if nil != encodeErr {
        t.Fatalf("encode: %v", encodeErr)
    }

    source := NewHmacTokenSource(&customHmacSecretProvider{secret: shortSecret}, hmacTestApplications(), HmacTokenSourceConfig{})

    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal", nil, DefaultHmacHeaderName, headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    cause := expectUnavailableToken(t, token)

    context := exception.LogContext(cause, exception.LogContextConfig{})
    rendered := fmt.Sprintf("%v", context)
    for _, expected := range []string{"argument:secrets", "key-custom", "secretLength:19", "minimumLength:32"} {
        if false == strings.Contains(rendered, expected) {
            t.Fatalf("expected the short secret named by field, key id and length in the cause, missing %q in %s", expected, rendered)
        }
    }

    if true == strings.Contains(rendered, string(shortSecret)) {
        t.Fatalf("expected the secret never written, got %s", rendered)
    }

    longSecret := []byte(thirtyTwoByteSecret)
    envelope.Nonce = "nonce-long"
    longHeaderValue, _ := encodeHmacHeaderValue("key-custom", envelope, longSecret)
    longSource := NewHmacTokenSource(&customHmacSecretProvider{secret: longSecret}, hmacTestApplications(), HmacTokenSourceConfig{})

    longToken, _ := longSource.Resolve(testRuntime(), hmacRequest("GET", "/internal", nil, DefaultHmacHeaderName, longHeaderValue))
    if false == longToken.IsAuthenticated() {
        t.Fatalf("expected a 32-byte custom secret to verify")
    }

    raisedSource := NewHmacTokenSource(&customHmacSecretProvider{secret: longSecret}, hmacTestApplications(), HmacTokenSourceConfig{MinimumSecretLength: bound.Of(33)})
    envelope.Nonce = "nonce-raised"
    raisedHeaderValue, _ := encodeHmacHeaderValue("key-custom", envelope, longSecret)
    if raisedToken, _ := raisedSource.Resolve(testRuntime(), hmacRequest("GET", "/internal", nil, DefaultHmacHeaderName, raisedHeaderValue)); true == raisedToken.IsAuthenticated() {
        t.Fatalf("expected a raised minimum to refuse the 32-byte secret")
    }

    expectPanicNamingField(t, "HmacTokenSourceConfig.MinimumSecretLength", func() {
        NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{MinimumSecretLength: bound.Of(31)})
    })
}

func TestHmacEnvelopeSigner_RefusesToSignWithAShortSecret(t *testing.T) {
    expectShortSecretRefusal(t, "argument", "secrets", 19, 32, "key-custom", func() {
        NewHmacEnvelopeSigner(hmacTestRuntime(), &customHmacSecretProvider{secret: []byte("short-custom-secret")}, "wms-service", HmacEnvelopeSignerConfig{})
    })

    rotatingProvider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), rotatingProvider, "wms-service", HmacEnvelopeSignerConfig{})
    rotatingProvider.secret = []byte("short-custom-secret")

    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal", nil, nil)
    if "" != headerValue {
        t.Fatalf("expected nothing signed, got %q", headerValue)
    }

    refusal, isRefusal := signErr.(*exception.Error)
    if false == isRefusal || "secrets" != refusal.Context()["argument"] || "key-custom" != refusal.Context()["keyId"] || 19 != refusal.Context()["secretLength"] {
        t.Fatalf("expected the short secret refused naming the field and the key id, got %v", signErr)
    }

    longSigner := NewHmacEnvelopeSigner(hmacTestRuntime(), &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}, "wms-service", HmacEnvelopeSignerConfig{})
    if _, longErr := longSigner.Sign(newTestRuntime(), "GET", "/internal", nil, nil); nil != longErr {
        t.Fatalf("expected a 32-byte secret to sign, got %v", longErr)
    }

    expectPanicNamingField(t, "HmacEnvelopeSignerConfig.MinimumSecretLength", func() {
        NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{MinimumSecretLength: bound.UnlimitedCount()})
    })
}

/* hmacTestRuntime is a run's runtime over a scope of its own, the shape a provider door is asked under outside a request */
func hmacTestRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}
