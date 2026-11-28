package security

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func hmacTestSecrets() *StaticHmacSecretProvider {
    return NewStaticHmacSecretProvider(HmacKeyRing{
        CurrentKeyId: "key-current",
        Keys: map[string]HmacKey{
            "key-current":  {Application: "wms-service", Secret: []byte("current-shared-secret-value-0001")},
            "key-previous": {Application: "wms-service", Secret: []byte("previous-shared-secret-value-002")},
        },
    })
}

func hmacTestApplications() *StaticHmacApplicationRegistry {
    return NewStaticHmacApplicationRegistry(map[string][]string{
        "wms-service": {"ROLE_SERVICE", "ROLE_WMS"},
    })
}

func hmacTestSource(guard securitycontract.NonceGuard) *HmacTokenSource {
    return NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   guard,
    })
}

func hmacStrictTestSource(guard securitycontract.NonceGuard) *HmacTokenSource {
    return NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   guard,
        Leeway:       strictLeeway,
    })
}

func hmacRequest(method string, path string, body []byte, headerName string, headerValue string) httpcontract.Request {
    var reader io.Reader
    if 0 < len(body) {
        reader = bytes.NewReader(body)
    }

    request := httptest.NewRequest(method, path, reader)
    if "" != headerValue {
        request.Header.Set(headerName, headerValue)
    }

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

func TestHmacTokenSource_ValidEnvelopeAuthenticatesAsServiceWithActor(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    actor := NewActor("user-7", securitycontract.ActorTypeUser, ActorConfig{Roles: []string{"ROLE_BUYER"}, Attributes: map[string]string{"tenant": "acme"}})
    body := []byte(`{"sku":"X-1"}`)

    headerValue, signErr := signer.Sign(newTestRuntime(), "POST", "/internal/orders", body, SignOptions{Actor: actor})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("POST", "/internal/orders", body, signer.HeaderName(), headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatal("expected the service to authenticate")
    }

    if "wms-service" != token.UserIdentifier() {
        t.Fatalf("expected principal wms-service, got %q", token.UserIdentifier())
    }

    if 2 != len(token.Roles()) {
        t.Fatalf("expected service roles from the registry, got %v", token.Roles())
    }

    resolvedActor, present := ActorFromToken(token)
    if false == present || "user-7" != resolvedActor.Identifier() {
        t.Fatalf("expected the originating actor to be propagated, got present=%v", present)
    }
}

func TestHmacTokenSource_MatchingAudienceAuthenticates(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{Audience: "orders-service"})

    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:      NewMemoryNonceGuard(MemoryNonceGuardConfig{}),
        ServiceIdentity: "orders-service",
    })

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if false == token.IsAuthenticated() {
        t.Fatal("expected an envelope addressed to this service to authenticate")
    }
}

func TestHmacTokenSource_MismatchedAudienceIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{Audience: "billing-service"})

    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:      NewMemoryNonceGuard(MemoryNonceGuardConfig{}),
        ServiceIdentity: "orders-service",
    })

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope minted for another service to be rejected")
    }
}

func TestHmacTokenSource_AudienceUnenforcedWhenNoServiceIdentity(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})

    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if false == token.IsAuthenticated() {
        t.Fatal("expected audience enforcement to be off when no ServiceIdentity is configured")
    }
}

func TestHmacTokenSource_TamperedSignatureIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})

    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})
    tampered := tamperHmacPayload(headerValue)

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), tampered))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a tampered envelope to be rejected")
    }
}

func TestHmacTokenSource_ExpiredEnvelopeIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{Clock: clock.NewFrozenClock(time.Now().Add(-time.Hour))})

    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an expired envelope to be rejected")
    }
}

func TestHmacTokenSource_ReplayedNonceIsRejected(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    first, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of the envelope to authenticate")
    }

    second, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if true == second.IsAuthenticated() {
        t.Fatal("expected the replayed envelope to be rejected")
    }
}

func TestHmacTokenSource_UnknownApplicationIsAnonymous(t *testing.T) {
    ghostKeys := map[string]HmacKey{"key-ghost": {Application: "ghost-service", Secret: []byte("ghost-shared-secret-value-00001-padded-to-the-hs256-minimum")}}

    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), NewStaticHmacSecretProvider(HmacKeyRing{CurrentKeyId: "key-ghost", Keys: ghostKeys}), "ghost-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := NewHmacTokenSource(NewStaticHmacSecretProvider(HmacKeyRing{CurrentKeyId: "key-ghost", Keys: ghostKeys}), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   NewMemoryNonceGuard(MemoryNonceGuardConfig{}),
    })
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an unregistered application to be rejected")
    }
}

func TestHmacTokenSource_CrossApplicationClaimWithValidKeyIsAnonymous(t *testing.T) {
    applications := NewStaticHmacApplicationRegistry(map[string][]string{
        "wms-service":   {"ROLE_SERVICE"},
        "admin-service": {"ROLE_ADMIN"},
    })
    source := NewHmacTokenSource(hmacTestSecrets(), applications, HmacTokenSourceConfig{NonceGuard: NewMemoryNonceGuard(MemoryNonceGuardConfig{})})

    /* forged directly: the real signer refuses to sign for an application its key is not bound to */
    forged := craftHmacHeaderValue(t, "key-current", []byte("current-shared-secret-value-0001"), "admin-service", "GET", "/internal/ping", nil)

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, forged))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope claiming an application its key id is not bound to be rejected")
    }
}

func craftHmacHeaderValue(t *testing.T, keyId string, secret []byte, application string, method string, path string, body []byte) string {
    t.Helper()

    nonce, nonceErr := newNonce()
    if nil != nonceErr {
        t.Fatalf("nonce: %v", nonceErr)
    }

    return craftHmacHeaderValueWithNonce(t, keyId, secret, application, method, path, body, nonce)
}

func craftHmacHeaderValueWithNonce(t *testing.T, keyId string, secret []byte, application string, method string, path string, body []byte, nonce string) string {
    t.Helper()

    now := time.Now()
    signedPath, signedQuery, _ := strings.Cut(path, "?")

    envelope := hmacEnvelope{
        Application: application,
        Method:      method,
        Path:        signedPath,
        Query:       signedQuery,
        IssuedAt:    now.Unix(),
        ExpiresAt:   now.Add(30 * time.Second).Unix(),
        Nonce:       nonce,
        BodyHash:    hashBody(body),
    }

    headerValue, encodeErr := encodeHmacHeaderValue(keyId, envelope, secret)
    if nil != encodeErr {
        t.Fatalf("encode: %v", encodeErr)
    }

    return headerValue
}

func TestHmacTokenSource_EndpointMismatchIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "POST", "/internal/orders", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    wrongPath, _ := source.Resolve(testRuntime(), hmacRequest("POST", "/internal/refunds", nil, signer.HeaderName(), headerValue))
    if true == wrongPath.IsAuthenticated() {
        t.Fatal("expected a path mismatch to be rejected")
    }

    wrongMethod, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/orders", nil, signer.HeaderName(), headerValue))
    if true == wrongMethod.IsAuthenticated() {
        t.Fatal("expected a method mismatch to be rejected")
    }
}

func TestHmacTokenSource_EndpointIsBoundToTheSpellingTheRouterRoutes(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    decodedHeader, _ := signer.Sign(newTestRuntime(), "GET", "/internal/files/a/b", nil, SignOptions{})
    crossed, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%2Fb", nil, signer.HeaderName(), decodedHeader))
    if true == crossed.IsAuthenticated() {
        t.Fatal("expected an envelope signed for /internal/files/a/b to be refused for the one-segment resource /internal/files/a%2Fb")
    }

    decodedBraceHeader, _ := signer.Sign(newTestRuntime(), "GET", "/internal/files/a/b{", nil, SignOptions{})
    crossedBrace, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%2Fb{", nil, signer.HeaderName(), decodedBraceHeader))
    if true == crossedBrace.IsAuthenticated() {
        t.Fatal("expected an envelope signed for /internal/files/a/b{ to be refused for the one-segment resource /internal/files/a%2Fb{")
    }

    routedHeader, _ := signer.Sign(newTestRuntime(), "GET", "/internal/files/a%2Fb", nil, SignOptions{})
    routed, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%2Fb", nil, signer.HeaderName(), routedHeader))
    if false == routed.IsAuthenticated() {
        t.Fatal("expected an envelope signed for the routed spelling /internal/files/a%2Fb to authenticate")
    }

    decodedSegmentHeader, _ := signer.Sign(newTestRuntime(), "GET", "/internal/files/café", nil, SignOptions{})
    decodedSegment, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/caf%C3%A9", nil, signer.HeaderName(), decodedSegmentHeader))
    if false == decodedSegment.IsAuthenticated() {
        t.Fatal("expected a segment that decodes to no separator to be bound by its decoded spelling, as before")
    }
}

func TestHmacTokenSource_RefusesASegmentThatDecodesToALiteralEncodedSeparator(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    routedHeader, _ := signer.Sign(newTestRuntime(), "GET", "/internal/files/a%2Fb", nil, SignOptions{})
    crossed, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%252Fb", nil, signer.HeaderName(), routedHeader))
    if true == crossed.IsAuthenticated() {
        t.Fatal("expected an envelope signed for the encoded separator /internal/files/a%2Fb to be refused for /internal/files/a%252Fb")
    }

    lowercaseLiteralHeader, _ := signer.Sign(newTestRuntime(), "GET", "/internal/files/a%2fb", nil, SignOptions{})
    lowercaseLiteral, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%252fb", nil, signer.HeaderName(), lowercaseLiteralHeader))
    if false == lowercaseLiteral.IsAuthenticated() {
        t.Fatal("expected a lowercase literal, which no encoded separator is routed as, to authenticate under its routed spelling")
    }

    literalPercentHeader, _ := signer.Sign(newTestRuntime(), "GET", "/internal/files/a%25b", nil, SignOptions{})
    literalPercent, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%2525b", nil, signer.HeaderName(), literalPercentHeader))
    if false == literalPercent.IsAuthenticated() {
        t.Fatal("expected a literal percent that does not spell an encoded separator to authenticate")
    }
}

func TestHmacTokenSource_BodyTamperingIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "POST", "/internal/orders", []byte(`{"sku":"X-1"}`), SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, _ := source.Resolve(
        testRuntime(),
        hmacRequest("POST", "/internal/orders", []byte(`{"sku":"X-999"}`), signer.HeaderName(), headerValue),
    )

    if true == token.IsAuthenticated() {
        t.Fatal("expected a tampered body to be rejected")
    }
}

func TestHmacTokenSource_QueryIsSignedAndMatched(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/orders?status=open&limit=50", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/orders?status=open&limit=50", nil, signer.HeaderName(), headerValue))

    if false == token.IsAuthenticated() {
        t.Fatal("expected a request whose query matches the signed envelope to authenticate")
    }
}

func TestHmacTokenSource_QueryTamperingIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/orders?status=open", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/orders?status=all", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a tampered query string to be rejected")
    }
}

func TestHmacTokenSource_RejectsEnvelopeTooCloseToExpiryForReplayGuard(t *testing.T) {
    source := hmacStrictTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    envelope := hmacEnvelope{Nonce: "n-1", ExpiresAt: time.Now().Add(-time.Second).Unix()}

    if guardErr := source.guardNonce(testRuntime(), "key-current", envelope); nil == guardErr {
        t.Fatal("expected an envelope past its guardable window to be rejected")
    }
}

func TestHmacTokenSource_NonceIsNamespacedAwayFromTotpGuard(t *testing.T) {
    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{})
    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{NonceGuard: guard})

    /* the exact key the TOTP authenticator would record for alice's next accepted code */
    totpGuardKey := "2fa:alice:000000"

    forged := craftHmacHeaderValueWithNonce(t, "key-current", []byte("current-shared-secret-value-0001"), "wms-service", "GET", "/internal/ping", nil, totpGuardKey)

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, forged))
    if false == token.IsAuthenticated() {
        t.Fatal("expected the crafted envelope to authenticate: it carries a valid key bound to the claimed application")
    }

    seen, rememberErr := guard.Remember(testRuntime(), totpGuardKey, bound.Of(time.Minute))
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if true == seen {
        t.Fatal("expected the TOTP guard key to be unseen: the HMAC nonce must be namespaced away from the 2fa key space")
    }
}

func TestHmacTokenSource_AcceptsPreviousActiveKey(t *testing.T) {
    previousSigner := NewHmacEnvelopeSigner(hmacTestRuntime(), NewStaticHmacSecretProvider(HmacKeyRing{CurrentKeyId: "key-previous", Keys: map[string]HmacKey{"key-previous": {Application: "wms-service", Secret: []byte("previous-shared-secret-value-002")}}}), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := previousSigner.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, previousSigner.HeaderName(), headerValue))

    if false == token.IsAuthenticated() {
        t.Fatal("expected a previous-but-active key to verify")
    }
}

func TestHmacTokenSource_RejectsUnknownKeyId(t *testing.T) {
    unknownSigner := NewHmacEnvelopeSigner(hmacTestRuntime(), NewStaticHmacSecretProvider(HmacKeyRing{CurrentKeyId: "key-retired", Keys: map[string]HmacKey{"key-retired": {Application: "wms-service", Secret: []byte("retired-shared-secret-value-0003")}}}), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := unknownSigner.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, unknownSigner.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope signed with an unknown key id to be rejected")
    }
}

func TestHmacTokenSource_MissingHeaderIsAnonymousWithoutError(t *testing.T) {
    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, ""))

    if nil != resolveErr {
        t.Fatalf("expected no error for a missing header, got %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatal("expected a missing header to resolve to anonymous")
    }
}

func TestHmacTokenSource_RestoresBodyForDownstreamHandler(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign(newTestRuntime(), "POST", "/internal/orders", body, SignOptions{})

    request := hmacRequest("POST", "/internal/orders", body, signer.HeaderName(), headerValue)

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    if _, resolveErr := source.Resolve(testRuntime(), request); nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    restored, readErr := io.ReadAll(request.HttpRequest().Body)
    if nil != readErr {
        t.Fatalf("read restored body: %v", readErr)
    }

    if false == bytes.Equal(body, restored) {
        t.Fatalf("expected the body to be restored for the handler, got %q", restored)
    }
}

func TestHmacTokenSource_RejectsExpiryBeyondMaxFutureExpiry(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{Ttl: bound.Of(time.Hour)})
    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:      NewMemoryNonceGuard(MemoryNonceGuardConfig{}),
        MaxFutureExpiry: bound.Of(time.Minute),
    })
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope expiring beyond the max future horizon to be rejected")
    }
}

func TestHmacTokenSource_TheDefaultHorizonIsAnHour(t *testing.T) {
    for _, probe := range []struct {
        ttl      time.Duration
        admitted bool
    }{
        {ttl: 59 * time.Minute, admitted: true},
        {ttl: 61 * time.Minute, admitted: false},
    } {
        signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{Ttl: bound.Of(probe.ttl)})
        headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

        source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
        token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

        if probe.admitted != token.IsAuthenticated() {
            t.Fatalf("expected an envelope expiring %v ahead to be admitted=%t at the default horizon", probe.ttl, probe.admitted)
        }
    }
}

func TestHmacTokenSource_BodyBeforeNonceSurvivesNonceBurn(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign(newTestRuntime(), "POST", "/internal/orders", body, SignOptions{})

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:            NewMemoryNonceGuard(MemoryNonceGuardConfig{}),
        VerifyBodyBeforeNonce: true,
    })

    burn, _ := source.Resolve(testRuntime(), hmacRequest("POST", "/internal/orders", []byte(`{"sku":"X-999"}`), signer.HeaderName(), headerValue))
    if true == burn.IsAuthenticated() {
        t.Fatal("expected the mutated-body replay to be rejected")
    }

    genuine, _ := source.Resolve(testRuntime(), hmacRequest("POST", "/internal/orders", body, signer.HeaderName(), headerValue))
    if false == genuine.IsAuthenticated() {
        t.Fatal("expected the genuine request to authenticate after a mutated-body replay attempt")
    }
}

func TestHmacTokenSource_NonceFirstBurnDeniesGenuineRequest(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign(newTestRuntime(), "POST", "/internal/orders", body, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    source.Resolve(testRuntime(), hmacRequest("POST", "/internal/orders", []byte(`{"sku":"X-999"}`), signer.HeaderName(), headerValue))

    genuine, _ := source.Resolve(testRuntime(), hmacRequest("POST", "/internal/orders", body, signer.HeaderName(), headerValue))
    if true == genuine.IsAuthenticated() {
        t.Fatal("expected the genuine request to be denied after the nonce was burned by a mutated-body replay")
    }
}

func hmacRequestUnderRoute(method string, path string, body []byte, headerName string, headerValue string, hmacCheckOrder httpcontract.HmacCheckOrder) httpcontract.Request {
    request := hmacRequest(method, path, body, headerName, headerValue)
    if httpcontract.HmacCheckOrderInherited != hmacCheckOrder {
        request.Attributes().Set(melodyhttp.RouteAttributeProtection, httpcontract.RouteProtectionConfig{HmacCheckOrder: hmacCheckOrder})
    }

    return request
}

func hmacGenuineSurvivesAMutatedReplay(t *testing.T, sourceBodyFirst bool, hmacCheckOrder httpcontract.HmacCheckOrder) bool {
    t.Helper()

    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign(newTestRuntime(), "POST", "/internal/orders", body, SignOptions{})

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:            NewMemoryNonceGuard(MemoryNonceGuardConfig{}),
        VerifyBodyBeforeNonce: sourceBodyFirst,
    })

    burn, _ := source.Resolve(testRuntime(), hmacRequestUnderRoute("POST", "/internal/orders", []byte(`{"sku":"X-999"}`), signer.HeaderName(), headerValue, hmacCheckOrder))
    if true == burn.IsAuthenticated() {
        t.Fatal("expected the mutated-body replay to be rejected")
    }

    genuine, _ := source.Resolve(testRuntime(), hmacRequestUnderRoute("POST", "/internal/orders", body, signer.HeaderName(), headerValue, hmacCheckOrder))

    return genuine.IsAuthenticated()
}

func TestHmacTokenSource_TheRoutesCheckOrderWinsInBothDirections(t *testing.T) {
    for _, testCase := range []struct {
        sourceBodyFirst bool
        hmacCheckOrder  httpcontract.HmacCheckOrder
        survives        bool
    }{
        {sourceBodyFirst: false, hmacCheckOrder: httpcontract.HmacCheckOrderBodyFirst, survives: true},
        {sourceBodyFirst: false, hmacCheckOrder: httpcontract.HmacCheckOrderNonceFirst, survives: false},
        {sourceBodyFirst: true, hmacCheckOrder: httpcontract.HmacCheckOrderBodyFirst, survives: true},
        {sourceBodyFirst: true, hmacCheckOrder: httpcontract.HmacCheckOrderNonceFirst, survives: false},
    } {
        if survives := hmacGenuineSurvivesAMutatedReplay(t, testCase.sourceBodyFirst, testCase.hmacCheckOrder); testCase.survives != survives {
            t.Fatalf("source body first %v, route %q: expected the genuine request to survive %v, got %v", testCase.sourceBodyFirst, testCase.hmacCheckOrder, testCase.survives, survives)
        }
    }
}

func TestHmacTokenSource_AnInheritedOrderIsTheSources(t *testing.T) {
    if true == hmacGenuineSurvivesAMutatedReplay(t, false, httpcontract.HmacCheckOrderInherited) {
        t.Fatal("expected a nonce-first source to keep its order on a route that inherits it")
    }

    if false == hmacGenuineSurvivesAMutatedReplay(t, true, httpcontract.HmacCheckOrderInherited) {
        t.Fatal("expected a body-first source to keep its order on a route that inherits it")
    }
}

func tamperHmacPayload(headerValue string) string {
    parts := strings.SplitN(headerValue, ".", 3)

    replacement := byte('A')
    if 'A' == parts[1][0] {
        replacement = 'B'
    }

    parts[1] = string(replacement) + parts[1][1:]

    return parts[0] + "." + parts[1] + "." + parts[2]
}

func hmacE2EFirewall(source securitycontract.TokenSource) *FirewallRegistry {
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "internal",
        Matcher:            &resolutionListenerTestMatcher{matches: true},
        MatcherDescription: "matcher:internal",
        TokenSource:        source,
        AccessControl:      NewAccessControl(nil),
        AccessDecisionManager: NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            []securitycontract.Voter{NewRoleHierarchyVoter(NewRoleHierarchy(map[string][]string{}), NewRoleVoter())}, AccessDecisionManagerConfig{}),
        RoleHierarchy:               NewRoleHierarchy(map[string][]string{}),
        LoginPath:                   "/login",
        LogoutPath:                  "/logout",
        RoleHierarchySource:         SourceFirewall,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    return NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
}

func TestHmacTokenSource_EndToEndResolvesServiceWithActorThroughFirewall(t *testing.T) {
    secrets := NewStaticHmacSecretProvider(HmacKeyRing{CurrentKeyId: "k1", Keys: map[string]HmacKey{"k1": {Application: "wms-service", Secret: []byte("hmac-e2e-shared-secret-value-padded-to-the-hs256-minimum")}}})
    applications := NewStaticHmacApplicationRegistry(map[string][]string{"wms-service": {"ROLE_SERVICE"}})
    source := NewHmacTokenSource(secrets, applications, HmacTokenSourceConfig{NonceGuard: NewMemoryNonceGuard(MemoryNonceGuardConfig{})})

    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), secrets, "wms-service", HmacEnvelopeSignerConfig{})
    actor := NewActor("user-7", securitycontract.ActorTypeUser, ActorConfig{Roles: []string{"ROLE_BUYER"}, Attributes: map[string]string{"tenant": "acme"}})

    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{Actor: actor})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    kernel := newTestKernel()
    RegisterKernelSecurityResolutionListener(kernel, hmacE2EFirewall(source))

    runtimeInstance := newTestRuntime()
    request := newSecurityTestRequest("GET", "/internal/ping", map[string]string{signer.HeaderName(): headerValue}, runtimeInstance)

    if _, dispatchErr := kernel.EventDispatcher().DispatchName(runtimeInstance, "kernel.request", melodyhttp.NewKernelRequestEvent(runtimeInstance, request)); nil != dispatchErr {
        t.Fatalf("dispatch: %v", dispatchErr)
    }

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatal("expected a security context on the runtime")
    }

    token := securityContext.Token()
    if false == token.IsAuthenticated() || "wms-service" != token.UserIdentifier() {
        t.Fatalf("expected the service to authenticate, got auth=%v id=%q", token.IsAuthenticated(), token.UserIdentifier())
    }

    resolvedActor, present := ActorFromToken(token)
    if false == present || "user-7" != resolvedActor.Identifier() || "acme" != resolvedActor.Attributes()["tenant"] {
        t.Fatalf("expected the originating actor to reach the security context, present=%v", present)
    }

    replayRuntime := newTestRuntime()
    replayRequest := newSecurityTestRequest("GET", "/internal/ping", map[string]string{signer.HeaderName(): headerValue}, replayRuntime)

    if _, dispatchErr := kernel.EventDispatcher().DispatchName(replayRuntime, "kernel.request", melodyhttp.NewKernelRequestEvent(replayRuntime, replayRequest)); nil != dispatchErr {
        t.Fatalf("replay dispatch: %v", dispatchErr)
    }

    replayContext, replayExists := SecurityContextFromRuntime(replayRuntime)
    if false == replayExists {
        t.Fatal("expected a security context for the replay")
    }

    if true == replayContext.Token().IsAuthenticated() {
        t.Fatal("expected the replayed envelope to resolve to an anonymous token")
    }
}

func TestHmacNonceGuardKey_ColonExtensionKeyIdsCannotCollide(t *testing.T) {
    if hmacNonceGuardKey("a", "b:nonce") == hmacNonceGuardKey("a:b", "nonce") {
        t.Fatalf("expected the guard keys of colon-extension key ids to differ")
    }

    guard := NewMemoryNonceGuard(MemoryNonceGuardConfig{})

    firstSeen, firstErr := guard.Remember(testRuntime(), hmacNonceGuardKey("a", "b:nonce"), bound.Of(time.Minute))
    if nil != firstErr || true == firstSeen {
        t.Fatalf("expected the first nonce to store, got seen=%t err=%v", firstSeen, firstErr)
    }

    secondSeen, secondErr := guard.Remember(testRuntime(), hmacNonceGuardKey("a:b", "nonce"), bound.Of(time.Minute))
    if nil != secondErr || true == secondSeen {
        t.Fatalf("expected key a:b's nonce to stay unburned, got seen=%t err=%v", secondSeen, secondErr)
    }
}

func TestHmacTokenSource_RejectsAnEnvelopeWithoutTheInternalAuthType(t *testing.T) {
    secret := []byte("current-shared-secret-value-0001")
    now := time.Now()

    payloadBytes, _ := json.Marshal(hmacEnvelope{
        Application: "wms-service",
        Method:      "GET",
        Path:        "/internal/ping",
        IssuedAt:    now.Unix(),
        ExpiresAt:   now.Add(30 * time.Second).Unix(),
        Nonce:       "nonce-1",
        BodyHash:    hashBody(nil),
    })

    headerBytes, _ := json.Marshal(map[string]string{"alg": "HS256", "kid": "key-current"})

    part0 := base64.RawURLEncoding.EncodeToString(headerBytes)
    part1 := base64.RawURLEncoding.EncodeToString(payloadBytes)
    signature := signHmacSha256(part0+"."+part1, secret)
    headerValue := part0 + "." + part1 + "." + base64.RawURLEncoding.EncodeToString(signature)

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatal("a correctly signed envelope without the internal-auth typ authenticated: nothing separates the envelope from any other HS256 credential")
    }
}

type failingNonceGuard struct {
    failure error
}

func (instance *failingNonceGuard) Remember(_ containercontract.Runtime, _ string, _ bound.Duration) (bool, error) {
    return false, instance.failure
}

func TestHmacTokenSource_ANonceGuardThatCannotAnswerIsUnavailable(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    guardErr := errors.New("redis is down")
    source := hmacTestSource(&failingNonceGuard{failure: guardErr})
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    cause := expectUnavailableToken(t, token)
    if false == errors.Is(cause, guardErr) {
        t.Fatalf("expected the guard's error in the cause chain, got %v", cause)
    }

    if 0 != len(logger.records) {
        t.Fatalf("expected no record from the source, the resolution listener files the one record, got %v", logger.records)
    }

    tampered, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), tamperHmacPayload(headerValue)))
    expectAnonymousToken(t, tampered)

    missing, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), ""))
    expectAnonymousToken(t, missing)

    replaySource := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    _, _ = replaySource.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    replayed, _ := replaySource.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    expectAnonymousToken(t, replayed)
}

func TestHmacTokenSource_AFullNonceGuardIsUnavailable(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    first, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})
    second, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{MaxEntries: bound.Of(1)}))

    if token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), first)); false == token.IsAuthenticated() {
        t.Fatal("expected the first envelope to authenticate while the guard has room")
    }

    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), second))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    expectUnavailableToken(t, token)
}

func TestHmacTokenSource_ForgedEnvelopeStaysAtInfo(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), tamperHmacPayload(headerValue)))
    if nil != resolveErr || true == token.IsAuthenticated() {
        t.Fatalf("a tampered envelope must resolve anonymous without error: authenticated=%v err=%v", token.IsAuthenticated(), resolveErr)
    }

    if 0 != len(logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatal("a forged envelope is routine noise and must not be filed at Error")
    }

    if 1 != len(logger.recordsAtLevel(loggingcontract.LevelInfo)) {
        t.Fatal("a forged envelope must be filed at Info")
    }
}

func TestHmacTokenSource_QueryMismatchContextCarriesNamesButNoValues(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal/ping?api_key=SIGNEDSECRETVALUE", nil, SignOptions{})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(
        runtimeInstance,
        hmacRequest("GET", "/internal/ping?api_key=PRESENTEDSECRETVALUE", nil, signer.HeaderName(), headerValue),
    )
    if nil != resolveErr || true == token.IsAuthenticated() {
        t.Fatalf("a query mismatch must resolve anonymous without error: authenticated=%v err=%v", token.IsAuthenticated(), resolveErr)
    }

    infoRecords := logger.recordsAtLevel(loggingcontract.LevelInfo)
    if 1 != len(infoRecords) {
        t.Fatalf("expected the rejection to be filed once at Info, got %d records", len(infoRecords))
    }

    rendered := fmt.Sprintf("%v", infoRecords[0].context)
    if true == strings.Contains(rendered, "SIGNEDSECRETVALUE") || true == strings.Contains(rendered, "PRESENTEDSECRETVALUE") {
        t.Fatalf("a query VALUE reached the journal: %s", rendered)
    }

    if false == strings.Contains(rendered, "api_key") {
        t.Fatalf("the parameter name is what makes the mismatch diagnosable and must survive the redaction: %s", rendered)
    }
}

/* decades from the real clock, so the envelope verifies only on the injected clocks */
func TestHmacTokenSource_TimeWindowRunsOnTheInjectedClock(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))

    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{Clock: frozen})
    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        Clock:        frozen,
    })

    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatal("an envelope inside its window on the injected clock was refused, so signer or source read some other clock")
    }

    frozen.Advance(time.Hour)

    lateToken, lateErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if nil != lateErr {
        t.Fatalf("resolve: %v", lateErr)
    }

    if true == lateToken.IsAuthenticated() {
        t.Fatal("the injected clock passed the expiry and the envelope still verified, so the source read some other clock")
    }
}

func TestNewHmacTokenSource_RefusesAMaxFutureExpiryUnderASecondOrUnlimited(t *testing.T) {
    for _, maxFutureExpiry := range []bound.Duration{bound.Of(-time.Minute), bound.Of(500 * time.Millisecond), bound.UnlimitedDuration()} {
        expectPanicNamingField(t, "HmacTokenSourceConfig.MaxFutureExpiry", func() {
            NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{MaxFutureExpiry: maxFutureExpiry})
        })
    }
}

func TestNewHmacTokenSource_RefusesANegativeLeeway(t *testing.T) {
    expectPanicNamingField(t, "HmacTokenSourceConfig.Leeway", func() {
        NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{Leeway: bound.Of(-time.Second)})
    })
}

func TestNewHmacTokenSource_RefusesAnUnlimitedOrTooWideLeeway(t *testing.T) {
    expectPanicNamingField(t, "HmacTokenSourceConfig.Leeway", func() {
        NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{Leeway: bound.UnlimitedDuration()})
    })

    expectPanicNamingField(t, "HmacTokenSourceConfig.Leeway", func() {
        NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{Leeway: bound.Of(5*time.Minute + time.Nanosecond)})
    })

    NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{Leeway: bound.Of(5 * time.Minute)})
}

func TestHmacTokenSource_TheDefaultLeewayIsFiveSeconds(t *testing.T) {
    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    expiry := time.Unix(1_700_000_000, 0)
    envelope := hmacEnvelope{ExpiresAt: expiry.Unix(), Nonce: "n1"}

    if timeErr := source.verifyTimeWindow(envelope, expiry.Add(5*time.Second)); nil != timeErr {
        t.Fatalf("expected an envelope 5s past its expiry accepted, got %v", timeErr)
    }

    timeErr := source.verifyTimeWindow(envelope, expiry.Add(5*time.Second+time.Nanosecond))
    if nil == timeErr || "internal-auth envelope is expired" != timeErr.Error() {
        t.Fatalf("expected an envelope just over 5s past its expiry refused, got %v", timeErr)
    }
}

func TestHmacTokenSource_TheNonceTtlCarriesTheLeeway(t *testing.T) {
    expiry := time.Unix(1_700_000_000, 0)
    frozen := clock.NewFrozenClock(expiry.Add(4 * time.Second))

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: frozen}),
        Clock:        frozen,
    })

    envelope := hmacEnvelope{ExpiresAt: expiry.Unix(), Nonce: "n1"}

    if guardErr := source.guardNonce(testRuntime(), "k1", envelope); nil != guardErr {
        t.Fatalf("expected the envelope 4s past its expiry guarded, got %v", guardErr)
    }

    replayErr := source.guardNonce(testRuntime(), "k1", envelope)
    if nil == replayErr || "internal-auth nonce has already been used" != replayErr.Error() {
        t.Fatalf("expected the replay 4s past the expiry refused as a replay, got %v", replayErr)
    }
}

func TestHmacTokenSource_TheGuardRefusesTheEnvelopeAtTheEndOfTheLeeway(t *testing.T) {
    expiry := time.Unix(1_700_000_000, 0)
    frozen := clock.NewFrozenClock(expiry.Add(5 * time.Second))

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: frozen}),
        Clock:        frozen,
    })

    guardErr := source.guardNonce(testRuntime(), "k1", hmacEnvelope{ExpiresAt: expiry.Unix(), Nonce: "n1"})
    if nil == guardErr || "internal-auth envelope is too close to expiry to guard against replay" != guardErr.Error() {
        t.Fatalf("expected the envelope at the end of the leeway refused by the guard, got %v", guardErr)
    }
}

func TestHmacTokenSource_TheTimeWindowAdmitsTheDeadlineTheNonceGuardRefuses(t *testing.T) {
    now := time.Unix(1_700_000_000, 0)
    frozen := clock.NewFrozenClock(now)

    /* injected: guardNonce measures the ttl against the source's clock, not the instant verifyTimeWindow is handed */
    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: frozen}),
        Clock:        frozen,
        Leeway:       strictLeeway,
    })

    envelope := hmacEnvelope{ExpiresAt: now.Unix(), Nonce: "n1"}

    if timeErr := source.verifyTimeWindow(envelope, now); nil != timeErr {
        t.Fatalf("expected the deadline instant itself to stay inside the accepted window, got %v", timeErr)
    }

    guardErr := source.guardNonce(testRuntime(), "k1", envelope)
    if nil == guardErr {
        t.Fatalf("expected the nonce guard to refuse an envelope it cannot record")
    }

    if "internal-auth envelope is too close to expiry to guard against replay" != guardErr.Error() {
        t.Fatalf("unexpected refusal message: %q", guardErr.Error())
    }
}

func TestHmacTokenSource_GuardsAnEnvelopeInItsLastSecondAndRefusesItsLastMillisecond(t *testing.T) {
    expiry := time.Unix(1_700_000_000, 0)
    frozen := clock.NewFrozenClock(expiry.Add(-500 * time.Millisecond))

    source := NewHmacTokenSource(hmacTestSecrets(), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   NewMemoryNonceGuard(MemoryNonceGuardConfig{Clock: frozen}),
        Clock:        frozen,
        Leeway:       strictLeeway,
    })

    envelope := hmacEnvelope{ExpiresAt: expiry.Unix(), Nonce: "n1"}

    if guardErr := source.guardNonce(testRuntime(), "k1", envelope); nil != guardErr {
        t.Fatalf("expected an envelope 500ms before its expiry to be guarded, got %v", guardErr)
    }

    if replayErr := source.guardNonce(testRuntime(), "k1", envelope); nil == replayErr || "internal-auth nonce has already been used" != replayErr.Error() {
        t.Fatalf("expected the replay inside the last second to be refused, got %v", replayErr)
    }

    frozen.Advance(500*time.Millisecond - 500*time.Microsecond)

    edgeErr := source.guardNonce(testRuntime(), "k1", hmacEnvelope{ExpiresAt: expiry.Unix(), Nonce: "n2"})
    if nil == edgeErr || "internal-auth envelope is too close to expiry to guard against replay" != edgeErr.Error() {
        t.Fatalf("expected a residual under a millisecond refused as the envelope's own failure, got %v", edgeErr)
    }
}

func TestHmacTokenSource_AnEnvelopePastTheDeadlineIsRefusedByTheTimeWindow(t *testing.T) {
    source := hmacStrictTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    now := time.Unix(1_700_000_000, 0)
    envelope := hmacEnvelope{ExpiresAt: now.Add(-time.Second).Unix(), Nonce: "n1"}

    timeErr := source.verifyTimeWindow(envelope, now)
    if nil == timeErr {
        t.Fatalf("expected an envelope a second past its deadline to be refused")
    }

    if "internal-auth envelope is expired" != timeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", timeErr.Error())
    }
}

func TestHmacTokenSource_AnEnvelopeIssuedInTheFutureIsRefused(t *testing.T) {
    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    now := time.Unix(1_700_000_000, 0)
    envelope := hmacEnvelope{
        IssuedAt:  now.Add(10 * time.Minute).Unix(),
        ExpiresAt: now.Add(20 * time.Minute).Unix(),
        Nonce:     "n1",
    }

    timeErr := source.verifyTimeWindow(envelope, now)
    if nil == timeErr {
        t.Fatalf("expected an envelope issued ten minutes in the future to be refused")
    }

    if "internal-auth envelope is not yet valid" != timeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", timeErr.Error())
    }
}

func TestHmacTokenSource_AMillisecondSkewAcrossASecondBoundaryIsAccepted(t *testing.T) {
    source := hmacStrictTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    boundary := time.Unix(1_700_000_000, 0)
    senderNow := boundary.Add(time.Millisecond)
    receiverNow := boundary.Add(-time.Millisecond)

    envelope := hmacEnvelope{
        IssuedAt:  senderNow.Unix(),
        ExpiresAt: senderNow.Add(30 * time.Second).Unix(),
        Nonce:     "n1",
    }

    if timeErr := source.verifyTimeWindow(envelope, receiverNow); nil != timeErr {
        t.Fatalf("expected a 2ms skew across a second boundary to be accepted, got %v", timeErr)
    }
}

func TestHmacTokenSource_AnEnvelopeIssuedMoreThanTheResolutionAheadIsRefused(t *testing.T) {
    source := hmacStrictTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    now := time.Unix(1_700_000_000, 0)
    envelope := hmacEnvelope{
        IssuedAt:  now.Add(2 * time.Second).Unix(),
        ExpiresAt: now.Add(time.Minute).Unix(),
        Nonce:     "n1",
    }

    timeErr := source.verifyTimeWindow(envelope, now)
    if nil == timeErr || "internal-auth envelope is not yet valid" != timeErr.Error() {
        t.Fatalf("expected an envelope issued two seconds ahead to be refused as not yet valid, got %v", timeErr)
    }
}

/* the tolerance is exactly one second: accepted at the start of the receiver's second, refused a millisecond before */
func TestHmacTokenSource_TheIssuedAtToleranceIsExactlyOneSecond(t *testing.T) {
    source := hmacStrictTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    now := time.Unix(1_700_000_000, 0)
    envelope := hmacEnvelope{
        IssuedAt:  now.Add(time.Second).Unix(),
        ExpiresAt: now.Add(time.Minute).Unix(),
        Nonce:     "n1",
    }

    if timeErr := source.verifyTimeWindow(envelope, now); nil != timeErr {
        t.Fatalf("expected an envelope one second ahead accepted at the start of the receiver's second, got %v", timeErr)
    }

    timeErr := source.verifyTimeWindow(envelope, now.Add(-time.Millisecond))
    if nil == timeErr || "internal-auth envelope is not yet valid" != timeErr.Error() {
        t.Fatalf("expected an envelope one second and a millisecond ahead refused, got %v", timeErr)
    }
}

func TestHmacTokenSource_TheIssuedAtToleranceDoesNotWidenTheExpiry(t *testing.T) {
    source := hmacStrictTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))

    now := time.Unix(1_700_000_000, 0)
    envelope := hmacEnvelope{
        IssuedAt:  now.Add(-time.Minute).Unix(),
        ExpiresAt: now.Add(-time.Second).Unix(),
        Nonce:     "n1",
    }

    timeErr := source.verifyTimeWindow(envelope, now)
    if nil == timeErr || "internal-auth envelope is expired" != timeErr.Error() {
        t.Fatalf("expected an envelope a second past its expiry to be refused, got %v", timeErr)
    }
}

func TestHmacTokenSource_QueryMismatchByReorderingRendersTwoDifferentQueries(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal/ping?b=2&a=1", nil, SignOptions{})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(
        runtimeInstance,
        hmacRequest("GET", "/internal/ping?a=1&b=2", nil, signer.HeaderName(), headerValue),
    )
    if nil != resolveErr || true == token.IsAuthenticated() {
        t.Fatalf("a reordered query must resolve anonymous without error: authenticated=%v err=%v", token.IsAuthenticated(), resolveErr)
    }

    infoRecords := logger.recordsAtLevel(loggingcontract.LevelInfo)
    if 1 != len(infoRecords) {
        t.Fatalf("expected the rejection to be filed once at Info, got %d records", len(infoRecords))
    }

    signed, request := infoRecords[0].context["signed"], infoRecords[0].context["request"]
    if signed == request {
        t.Fatalf("expected the signed and the request query to render differently, both rendered %v", signed)
    }
}

func TestHmacTokenSource_AnUnknownApplicationIsJournaledByItsName(t *testing.T) {
    ghostKeys := map[string]HmacKey{"key-ghost": {Application: "ghost-service", Secret: []byte("ghost-shared-secret-value-00001-padded-to-the-hs256-minimum")}}
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), NewStaticHmacSecretProvider(HmacKeyRing{CurrentKeyId: "key-ghost", Keys: ghostKeys}), "ghost-service", HmacEnvelopeSignerConfig{})
    headerValue, _ := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})

    source := NewHmacTokenSource(NewStaticHmacSecretProvider(HmacKeyRing{CurrentKeyId: "key-ghost", Keys: ghostKeys}), hmacTestApplications(), HmacTokenSourceConfig{
        NonceGuard:   NewMemoryNonceGuard(MemoryNonceGuardConfig{}),
    })
    runtimeInstance, logger := runtimeWithRecordingLogger()
    _, _ = source.Resolve(runtimeInstance, hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    rejection := hmacRejectionContext(t, logger)
    if "internal-auth application is not known" != rejection["error"] || "ghost-service" != rejection["application"] {
        t.Fatalf("expected the rejection journaled with the application named, got %v", rejection)
    }
}

func TestHmacTokenSource_ACrossApplicationClaimIsJournaledWithBothApplications(t *testing.T) {
    secrets := hmacTestSecrets()
    source := NewHmacTokenSource(secrets, hmacTestApplications(), HmacTokenSourceConfig{NonceGuard: NewMemoryNonceGuard(MemoryNonceGuardConfig{})})
    secret, _, _ := secrets.Secret(hmacTestRuntime(), "key-current")
    headerValue := craftHmacHeaderValue(t, "key-current", secret, "admin-service", "GET", "/internal/ping", nil)

    runtimeInstance, logger := runtimeWithRecordingLogger()
    _, _ = source.Resolve(runtimeInstance, hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, headerValue))

    rejection := hmacRejectionContext(t, logger)
    if "admin-service" != rejection["claimedApplication"] || "" == rejection["boundApplication"] {
        t.Fatalf("expected the claimed and the bound application named, got %v", rejection)
    }
}

func hmacRejectionContext(t *testing.T, logger *recordingLogger) map[string]any {
    t.Helper()

    for _, record := range logger.records {
        if "internal-auth envelope rejected" == record.message {
            return record.context
        }
    }

    t.Fatalf("expected the rejection journaled, got %v", logger.records)

    return nil
}

type recordingHmacApplicationRegistry struct {
    failure      error
    seenRuntimes []containercontract.Runtime
}

func (instance *recordingHmacApplicationRegistry) RolesForApplication(runtimeInstance containercontract.Runtime, application string) ([]string, bool, error) {
    instance.seenRuntimes = append(instance.seenRuntimes, runtimeInstance)

    if nil != instance.failure {
        return nil, false, instance.failure
    }

    return []string{"ROLE_SERVICE"}, "wms-service" == application, nil
}

func signedWithCustomProvider(t *testing.T, provider *customHmacSecretProvider) (string, string) {
    t.Helper()

    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), provider, "wms-service", HmacEnvelopeSignerConfig{})
    headerValue, signErr := signer.Sign(newTestRuntime(), "GET", "/internal/ping", nil, SignOptions{})
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    return signer.HeaderName(), headerValue
}

func expectUnavailableFromFailingHmacDoor(t *testing.T, source *HmacTokenSource, headerName string, headerValue string, failure error) {
    t.Helper()

    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, hmacRequest("GET", "/internal/ping", nil, headerName, headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    cause := expectUnavailableToken(t, token)
    if false == errors.Is(cause, failure) || false == exception.AnswersUnavailable(cause) {
        t.Fatalf("expected the door's failure in a cause answered unavailable, got %v", cause)
    }

    if 0 != len(logger.recordsAtLevel(loggingcontract.LevelInfo)) {
        t.Fatalf("expected no rejected-envelope record, got %v", logger.records)
    }
}

func TestHmacTokenSource_AProviderThatCannotAnswerTheSecretIsTheUnavailableToken(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    headerName, headerValue := signedWithCustomProvider(t, provider)

    failure := errors.New("the secret manager is down")
    provider.secretErr = failure

    source := NewHmacTokenSource(provider, hmacTestApplications(), HmacTokenSourceConfig{})
    expectUnavailableFromFailingHmacDoor(t, source, headerName, headerValue, failure)
}

func TestHmacTokenSource_AProviderThatCannotAnswerApplicationForKeyIdIsTheUnavailableToken(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    headerName, headerValue := signedWithCustomProvider(t, provider)

    failure := errors.New("the secret manager is down")
    provider.applicationErr = failure

    source := NewHmacTokenSource(provider, hmacTestApplications(), HmacTokenSourceConfig{})
    expectUnavailableFromFailingHmacDoor(t, source, headerName, headerValue, failure)
}

func TestHmacTokenSource_AnApplicationRegistryThatCannotAnswerIsTheUnavailableToken(t *testing.T) {
    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    headerName, headerValue := signedWithCustomProvider(t, provider)

    failure := errors.New("the registry is down")
    source := NewHmacTokenSource(provider, &recordingHmacApplicationRegistry{failure: failure}, HmacTokenSourceConfig{})
    expectUnavailableFromFailingHmacDoor(t, source, headerName, headerValue, failure)
}

func TestHmacTokenSource_AnUnknownKeyIdStaysAnonymous(t *testing.T) {
    signingProvider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret), keyId: "key-elsewhere"}
    headerName, headerValue := signedWithCustomProvider(t, signingProvider)

    source := NewHmacTokenSource(&customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}, hmacTestApplications(), HmacTokenSourceConfig{})

    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, headerName, headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    expectAnonymousToken(t, token)
}

func TestHmacTokenSource_HandsTheProviderAndTheRegistryTheSameRequestRuntime(t *testing.T) {
    signingProvider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    headerName, headerValue := signedWithCustomProvider(t, signingProvider)

    provider := &customHmacSecretProvider{secret: []byte(thirtyTwoByteSecret)}
    registry := &recordingHmacApplicationRegistry{}
    source := NewHmacTokenSource(provider, registry, HmacTokenSourceConfig{})
    requestRuntime := testRuntime()

    token, resolveErr := source.Resolve(requestRuntime, hmacRequest("GET", "/internal/ping", nil, headerName, headerValue))
    if nil != resolveErr || false == token.IsAuthenticated() {
        t.Fatalf("expected the envelope to authenticate, got %v", resolveErr)
    }

    providerRuntimes := provider.runtimesSeen()
    if 2 != len(providerRuntimes) || 1 != len(registry.seenRuntimes) {
        t.Fatalf("expected the secret, the binding and the roles read once each, got %d %d", len(providerRuntimes), len(registry.seenRuntimes))
    }

    for _, seen := range providerRuntimes {
        if requestRuntime != seen {
            t.Fatalf("expected the provider handed the request runtime")
        }
    }

    if requestRuntime != registry.seenRuntimes[0] {
        t.Fatalf("expected the registry handed the request runtime")
    }
}

func TestHmacTokenSource_ReadsAnActorSignedWithTheLegacySpelling(t *testing.T) {
    secrets := hmacTestSecrets()
    secret, _, _ := secrets.Secret(hmacTestRuntime(), "key-current")
    now := time.Now()

    headerBytes, _ := json.Marshal(hmacEnvelopeHeader{Algorithm: hmacEnvelopeAlgorithm, Type: hmacEnvelopeType, KeyId: "key-current"})
    payload := fmt.Sprintf(
        `{"app":"wms-service","method":"GET","path":"/internal/ping","iat":%d,"exp":%d,"nonce":"legacy-actor-nonce","bodyHash":%q,"actor":{"Identifier":"user-7","Type":"user","Roles":["ROLE_BUYER"],"Attributes":{"tenant":"acme"}}}`,
        now.Unix(),
        now.Add(30*time.Second).Unix(),
        hashBody(nil),
    )

    part0 := base64.RawURLEncoding.EncodeToString(headerBytes)
    part1 := base64.RawURLEncoding.EncodeToString([]byte(payload))
    headerValue := part0 + "." + part1 + "." + base64.RawURLEncoding.EncodeToString(signHmacSha256(part0+"."+part1, secret))

    source := NewHmacTokenSource(secrets, hmacTestApplications(), HmacTokenSourceConfig{})
    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, headerValue))
    if nil != resolveErr || false == token.IsAuthenticated() {
        t.Fatalf("expected the legacy envelope to authenticate, got %v", resolveErr)
    }

    actor, hasActor := ActorFromToken(token)
    if false == hasActor || "user-7" != actor.Identifier() || "acme" != actor.Attributes()["tenant"] || 1 != len(actor.Roles()) {
        t.Fatalf("expected the PascalCase actor read, got %v", actor)
    }
}

func TestHmacTokenSource_IsStateless(t *testing.T) {
    if false == hmacTestSource(nil).Stateless() {
        t.Fatalf("expected the hmac source to answer stateless")
    }
}

func TestNewHmacTokenSource_RefusesANilRequiredArgumentByName(t *testing.T) {
    cases := []struct {
        name         string
        secrets      HmacSecretProvider
        applications HmacApplicationRegistry
        argument     string
    }{
        {"a nil secrets provider", nil, hmacTestApplications(), "secrets"},
        {"a nil application registry", hmacTestSecrets(), nil, "applications"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            expectPanicNamingTheArgument(t, testCase.argument, func() {
                NewHmacTokenSource(testCase.secrets, testCase.applications, HmacTokenSourceConfig{})
            })
        })
    }
}

func TestHmacTokenSource_UrlEncodedBody_HashedFromTheRawBytes_ThenStillParsable(t *testing.T) {
    signer := NewHmacEnvelopeSigner(hmacTestRuntime(), hmacTestSecrets(), "wms-service", HmacEnvelopeSignerConfig{})
    body := []byte("sku=X-1&quantity=2")

    for _, formFirst := range []bool{false, true} {
        headerValue, _ := signer.Sign(newTestRuntime(), "POST", "/internal/orders", body, SignOptions{})
        netRequest := httptest.NewRequest("POST", "/internal/orders", bytes.NewReader(body))
        netRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        netRequest.Header.Set(signer.HeaderName(), headerValue)
        request := melodyhttp.NewRequest(netRequest, testRuntime(), melodyhttp.RequestConfig{MaxBodyBytes: bound.Of(1024)})

        if true == formFirst {
            if _, formErr := request.Post(); nil != formErr {
                t.Fatalf("form read: %v", formErr)
            }
        }

        source := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{}))
        token, _ := source.Resolve(testRuntime(), request)
        if false == token.IsAuthenticated() {
            t.Fatalf("expected the envelope verified over the raw bytes (form read first: %t)", formFirst)
        }

        postBag, formErr := request.Post()
        if nil != formErr || false == postBag.Has("sku") {
            t.Fatalf("expected the form parsable after the check (form read first: %t), got %v", formFirst, formErr)
        }
    }
}
