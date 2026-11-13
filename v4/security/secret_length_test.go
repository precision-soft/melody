package security

import (
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

const thirtyTwoByteSecret = "a-secret-of-exactly-32-bytes-ok!"

type customHmacSecretProvider struct {
    secret []byte
}

func (instance *customHmacSecretProvider) CurrentKeyId() string {
    return "key-custom"
}

func (instance *customHmacSecretProvider) Secret(keyId string) ([]byte, bool) {
    if "key-custom" != keyId {
        return nil, false
    }

    return append([]byte{}, instance.secret...), true
}

func (instance *customHmacSecretProvider) ApplicationForKeyId(keyId string) (string, bool) {
    if "key-custom" != keyId {
        return "", false
    }

    return "wms-service", true
}

func expectShortSecretRefusal(t *testing.T, field string, secretLength int, minimumLength int, keyId string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected a refusal naming %s", field)
        }

        context := refusal.Context()
        if field != context["field"] || secretLength != context["secretLength"] || minimumLength != context["minimumLength"] {
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

func TestNewJwtTokenValidator_RefusesASecretShorterThanTheMinimum(t *testing.T) {
    expectShortSecretRefusal(t, "JwtConfig.Secret", 31, 32, "", func() {
        NewJwtTokenValidator(JwtConfig{Secret: []byte("short-secret-of-31-bytes-long!!")})
    })

    NewJwtTokenValidator(JwtConfig{Secret: []byte(thirtyTwoByteSecret)})
}

func TestNewJwtTokenValidator_MinimumSecretLengthRaisesTheFloorOnly(t *testing.T) {
    expectShortSecretRefusal(t, "JwtConfig.Secret", 32, 40, "", func() {
        NewJwtTokenValidator(JwtConfig{Secret: []byte(thirtyTwoByteSecret), MinimumSecretLength: bound.Of(40)})
    })

    NewJwtTokenValidator(JwtConfig{Secret: []byte(strings.Repeat("k", 40)), MinimumSecretLength: bound.Of(40)})

    expectPanicNamingField(t, "JwtConfig.MinimumSecretLength", func() {
        NewJwtTokenValidator(JwtConfig{Secret: []byte(thirtyTwoByteSecret), MinimumSecretLength: bound.Of(31)})
    })
    expectPanicNamingField(t, "JwtConfig.MinimumSecretLength", func() {
        NewJwtTokenValidator(JwtConfig{Secret: []byte(thirtyTwoByteSecret), MinimumSecretLength: bound.UnlimitedCount()})
    })
}

func TestNewStaticHmacSecretProvider_RefusesAShortSecretOfAnyKey(t *testing.T) {
    expectShortSecretRefusal(t, "HmacKeyRing.Keys", 9, 32, "key-previous", func() {
        NewStaticHmacSecretProvider(HmacKeyRing{
            CurrentKeyId: "key-current",
            Keys: map[string]HmacKey{
                "key-current":  {Application: "wms-service", Secret: []byte("current-shared-secret-value-0001")},
                "key-previous": {Application: "wms-service", Secret: []byte("short-key")},
            },
        })
    })

    expectShortSecretRefusal(t, "HmacKeyRing.Keys", 32, 48, "key-current", func() {
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

    source := NewHmacTokenSource(HmacTokenSourceConfig{Secrets: &customHmacSecretProvider{secret: shortSecret}, Applications: hmacTestApplications()})

    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal", nil, DefaultHmacHeaderName, headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    cause := expectUnavailableToken(t, token)

    context := exception.LogContext(cause, exception.LogContextConfig{})
    rendered := fmt.Sprintf("%v", context)
    for _, expected := range []string{"HmacTokenSourceConfig.Secrets", "key-custom", "secretLength:19", "minimumLength:32"} {
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
    longSource := NewHmacTokenSource(HmacTokenSourceConfig{Secrets: &customHmacSecretProvider{secret: longSecret}, Applications: hmacTestApplications()})

    longToken, _ := longSource.Resolve(testRuntime(), hmacRequest("GET", "/internal", nil, DefaultHmacHeaderName, longHeaderValue))
    if false == longToken.IsAuthenticated() {
        t.Fatalf("expected a 32-byte custom secret to verify")
    }

    raisedSource := NewHmacTokenSource(HmacTokenSourceConfig{Secrets: &customHmacSecretProvider{secret: longSecret}, Applications: hmacTestApplications(), MinimumSecretLength: bound.Of(33)})
    envelope.Nonce = "nonce-raised"
    raisedHeaderValue, _ := encodeHmacHeaderValue("key-custom", envelope, longSecret)
    if raisedToken, _ := raisedSource.Resolve(testRuntime(), hmacRequest("GET", "/internal", nil, DefaultHmacHeaderName, raisedHeaderValue)); true == raisedToken.IsAuthenticated() {
        t.Fatalf("expected a raised minimum to refuse the 32-byte secret")
    }

    expectPanicNamingField(t, "HmacTokenSourceConfig.MinimumSecretLength", func() {
        NewHmacTokenSource(HmacTokenSourceConfig{Secrets: hmacTestSecrets(), Applications: hmacTestApplications(), MinimumSecretLength: bound.Of(31)})
    })
}

func TestHmacEnvelopeSigner_RefusesToSignWithAShortSecret(t *testing.T) {
    expectShortSecretRefusal(t, "HmacEnvelopeSignerConfig.Secrets", 19, 32, "key-custom", func() {
        NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{Application: "wms-service", Secrets: &customHmacSecretProvider{secret: []byte("short-custom-secret")}})
    })

    rotatingProvider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{Application: "wms-service", Secrets: rotatingProvider})
    rotatingProvider.secret = []byte("short-custom-secret")

    headerValue, signErr := signer.Sign("GET", "/internal", nil, nil)
    if "" != headerValue {
        t.Fatalf("expected nothing signed, got %q", headerValue)
    }

    refusal, isRefusal := signErr.(*exception.Error)
    if false == isRefusal || "HmacEnvelopeSignerConfig.Secrets" != refusal.Context()["field"] || "key-custom" != refusal.Context()["keyId"] || 19 != refusal.Context()["secretLength"] {
        t.Fatalf("expected the short secret refused naming the field and the key id, got %v", signErr)
    }

    longSigner := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{Application: "wms-service", Secrets: &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}})
    if _, longErr := longSigner.Sign("GET", "/internal", nil, nil); nil != longErr {
        t.Fatalf("expected a 32-byte secret to sign, got %v", longErr)
    }

    expectPanicNamingField(t, "HmacEnvelopeSignerConfig.MinimumSecretLength", func() {
        NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{Application: "wms-service", Secrets: hmacTestSecrets(), MinimumSecretLength: bound.UnlimitedCount()})
    })
}
