package security

import (
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func hmacSignerProbeSecrets() *StaticHmacSecretProvider {
    return NewStaticHmacSecretProvider("key-current", map[string]HmacKey{
        "key-current": {App: "billing", Secret: []byte("current-shared-secret-value-0001")},
    })
}

func TestNewHmacEnvelopeSigner_RefusesASignerThatCanNotIdentifyItself(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "", Secrets: hmacSignerProbeSecrets()})
    }, "hmac signer app is empty")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "billing", Secrets: nil})
    }, "hmac signer secrets provider is nil")

    var typedNilSecrets HmacSecretProvider = (*StaticHmacSecretProvider)(nil)
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "billing", Secrets: typedNilSecrets})
    }, "hmac signer secrets provider is nil")
}

func TestNewHmacEnvelopeSigner_RefusesACurrentKeyIssuedToAnotherApp(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "crm", Secrets: hmacSignerProbeSecrets()})
    }, "hmac signer current key id is not bound to the signer app")
}

func TestNewHmacEnvelopeSigner_FallsBackToTheDefaultHeaderName(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "billing", Secrets: hmacSignerProbeSecrets()})

    if DefaultHmacHeaderName != signer.HeaderName() {
        t.Fatalf("expected the default header name, got %q", signer.HeaderName())
    }

    named := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "billing", Secrets: hmacSignerProbeSecrets(), HeaderName: "X-Own-Auth"})
    if "X-Own-Auth" != named.HeaderName() {
        t.Fatalf("expected the configured header name, got %q", named.HeaderName())
    }
}

func TestHmacEnvelopeSigner_NonPositiveTtlReachesTheDefault(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))

    for _, probe := range []time.Duration{0, -time.Minute} {
        signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{
            App:     "billing",
            Secrets: hmacSignerProbeSecrets(),
            Ttl:     probe,
            Clock:   frozen,
        })

        headerValue, signErr := signer.Sign("GET", "/orders", nil, nil)
        if nil != signErr {
            t.Fatalf("unexpected sign error: %v", signErr)
        }

        envelope, _, decodeErr := decodeHmacHeaderValue(headerValue, hmacSignerProbeSecrets())
        if nil != decodeErr {
            t.Fatalf("unexpected decode error: %v", decodeErr)
        }

        if envelope.ExpiresAt != int64(1000+defaultHmacSignerTtl/time.Second) {
            t.Fatalf("expected a ttl of %v for %v, got exp=%d iat=%d", defaultHmacSignerTtl, probe, envelope.ExpiresAt, envelope.IssuedAt)
        }
    }
}

func TestHmacEnvelopeSigner_SignSplitsTheQueryOffThePath(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{
        App:      "billing",
        Secrets:  hmacSignerProbeSecrets(),
        Audience: "warehouse",
        Clock:    clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    headerValue, signErr := signer.Sign("POST", "/orders?page=2&size=10", []byte("payload"), nil)
    if nil != signErr {
        t.Fatalf("unexpected sign error: %v", signErr)
    }

    envelope, keyId, decodeErr := decodeHmacHeaderValue(headerValue, hmacSignerProbeSecrets())
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

func TestHmacEnvelopeSigner_SignMintsAFreshNonceEachTime(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{
        App:     "billing",
        Secrets: hmacSignerProbeSecrets(),
        Clock:   clock.NewFrozenClock(time.Unix(1000, 0)),
    })

    firstValue, _ := signer.Sign("GET", "/orders", nil, nil)
    secondValue, _ := signer.Sign("GET", "/orders", nil, nil)

    first, _, _ := decodeHmacHeaderValue(firstValue, hmacSignerProbeSecrets())
    second, _, _ := decodeHmacHeaderValue(secondValue, hmacSignerProbeSecrets())

    if "" == first.Nonce || first.Nonce == second.Nonce {
        t.Fatalf("expected a fresh nonce per envelope, got %q twice", first.Nonce)
    }

    if true == strings.Contains(first.Nonce, "=") {
        t.Fatalf("expected an unpadded base64url nonce, got %q", first.Nonce)
    }
}
