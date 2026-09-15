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

    "github.com/precision-soft/melody/v3/clock"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func hmacTestSecrets() *StaticHmacSecretProvider {
    return NewStaticHmacSecretProvider(
        "key-current",
        map[string]HmacKey{
            "key-current":  {App: "wms-service", Secret: []byte("current-shared-secret-value-0001")},
            "key-previous": {App: "wms-service", Secret: []byte("previous-shared-secret-value-002")},
        },
    )
}

func hmacTestApps() *StaticHmacAppRegistry {
    return NewStaticHmacAppRegistry(map[string][]string{
        "wms-service": {"ROLE_SERVICE", "ROLE_WMS"},
    })
}

func hmacTestSource(guard securitycontract.NonceGuard) *HmacTokenSource {
    return NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:    hmacTestSecrets(),
        Apps:       hmacTestApps(),
        NonceGuard: guard,
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
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    actor := NewActor("user-7", securitycontract.ActorTypeUser, []string{"ROLE_BUYER"}, map[string]string{"tenant": "acme"})
    body := []byte(`{"sku":"X-1"}`)

    headerValue, signErr := signer.Sign("POST", "/internal/orders", body, actor)
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := hmacTestSource(NewMemoryNonceGuard())
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
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets(), Audience: "orders-service"})

    headerValue, signErr := signer.Sign("GET", "/internal/ping", nil, nil)
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:         hmacTestSecrets(),
        Apps:            hmacTestApps(),
        NonceGuard:      NewMemoryNonceGuard(),
        ServiceIdentity: "orders-service",
    })

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if false == token.IsAuthenticated() {
        t.Fatal("expected an envelope addressed to this service to authenticate")
    }
}

func TestHmacTokenSource_MismatchedAudienceIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets(), Audience: "billing-service"})

    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:         hmacTestSecrets(),
        Apps:            hmacTestApps(),
        NonceGuard:      NewMemoryNonceGuard(),
        ServiceIdentity: "orders-service",
    })

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope minted for another service to be rejected")
    }
}

func TestHmacTokenSource_AudienceUnenforcedWhenNoServiceIdentity(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})

    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if false == token.IsAuthenticated() {
        t.Fatal("expected audience enforcement to be off when no ServiceIdentity is configured")
    }
}

func TestHmacTokenSource_TamperedSignatureIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})

    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)
    tampered := tamperHmacPayload(headerValue)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), tampered))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a tampered envelope to be rejected")
    }
}

func TestHmacTokenSource_ExpiredEnvelopeIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets(), Ttl: time.Nanosecond})

    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)
    time.Sleep(2 * time.Millisecond)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an expired envelope to be rejected")
    }
}

func TestHmacTokenSource_ReplayedNonceIsRejected(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())

    first, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if false == first.IsAuthenticated() {
        t.Fatal("expected the first use of the envelope to authenticate")
    }

    second, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if true == second.IsAuthenticated() {
        t.Fatal("expected the replayed envelope to be rejected")
    }
}

func TestHmacTokenSource_UnknownAppIsAnonymous(t *testing.T) {
    ghostKeys := map[string]HmacKey{"key-ghost": {App: "ghost-service", Secret: []byte("ghost-shared-secret-value-00001")}}

    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "ghost-service", Secrets: NewStaticHmacSecretProvider("key-ghost", ghostKeys)})
    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:    NewStaticHmacSecretProvider("key-ghost", ghostKeys),
        Apps:       hmacTestApps(),
        NonceGuard: NewMemoryNonceGuard(),
    })
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an unregistered app to be rejected")
    }
}

func TestHmacTokenSource_CrossAppClaimWithValidKeyIsAnonymous(t *testing.T) {
    apps := NewStaticHmacAppRegistry(map[string][]string{
        "wms-service":   {"ROLE_SERVICE"},
        "admin-service": {"ROLE_ADMIN"},
    })
    source := NewHmacTokenSource(HmacTokenSourceConfig{Secrets: hmacTestSecrets(), Apps: apps, NonceGuard: NewMemoryNonceGuard()})

    forged := craftHmacHeaderValue(t, "key-current", []byte("current-shared-secret-value-0001"), "admin-service", "GET", "/internal/ping", nil)

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, forged))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope claiming an app its key id is not bound to be rejected")
    }
}

func craftHmacHeaderValue(t *testing.T, keyId string, secret []byte, app string, method string, path string, body []byte) string {
    t.Helper()

    nonce, nonceErr := newNonce()
    if nil != nonceErr {
        t.Fatalf("nonce: %v", nonceErr)
    }

    return craftHmacHeaderValueWithNonce(t, keyId, secret, app, method, path, body, nonce)
}

func craftHmacHeaderValueWithNonce(t *testing.T, keyId string, secret []byte, app string, method string, path string, body []byte, nonce string) string {
    t.Helper()

    now := time.Now()
    signedPath, signedQuery, _ := strings.Cut(path, "?")

    envelope := hmacEnvelope{
        App:       app,
        Method:    method,
        Path:      signedPath,
        Query:     signedQuery,
        IssuedAt:  now.Unix(),
        ExpiresAt: now.Add(30 * time.Second).Unix(),
        Nonce:     nonce,
        BodyHash:  hashBody(body),
    }

    headerValue, encodeErr := encodeHmacHeaderValue(keyId, envelope, secret)
    if nil != encodeErr {
        t.Fatalf("encode: %v", encodeErr)
    }

    return headerValue
}

func TestHmacTokenSource_EndpointMismatchIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, _ := signer.Sign("POST", "/internal/orders", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())

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
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    source := hmacTestSource(NewMemoryNonceGuard())

    decodedHeader, _ := signer.Sign("GET", "/internal/files/a/b", nil, nil)
    crossed, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%2Fb", nil, signer.HeaderName(), decodedHeader))
    if true == crossed.IsAuthenticated() {
        t.Fatal("expected an envelope signed for /internal/files/a/b to be refused for the one-segment resource /internal/files/a%2Fb")
    }

    routedHeader, _ := signer.Sign("GET", "/internal/files/a%2Fb", nil, nil)
    routed, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/a%2Fb", nil, signer.HeaderName(), routedHeader))
    if false == routed.IsAuthenticated() {
        t.Fatal("expected an envelope signed for the routed spelling /internal/files/a%2Fb to authenticate")
    }

    decodedSegmentHeader, _ := signer.Sign("GET", "/internal/files/café", nil, nil)
    decodedSegment, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/files/caf%C3%A9", nil, signer.HeaderName(), decodedSegmentHeader))
    if false == decodedSegment.IsAuthenticated() {
        t.Fatal("expected a segment that decodes to no separator to be bound by its decoded spelling, as before")
    }
}

func TestHmacTokenSource_BodyTamperingIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, _ := signer.Sign("POST", "/internal/orders", []byte(`{"sku":"X-1"}`), nil)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(
        testRuntime(),
        hmacRequest("POST", "/internal/orders", []byte(`{"sku":"X-999"}`), signer.HeaderName(), headerValue),
    )

    if true == token.IsAuthenticated() {
        t.Fatal("expected a tampered body to be rejected")
    }
}

func TestHmacTokenSource_QueryIsSignedAndMatched(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, _ := signer.Sign("GET", "/internal/orders?status=open&limit=50", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/orders?status=open&limit=50", nil, signer.HeaderName(), headerValue))

    if false == token.IsAuthenticated() {
        t.Fatal("expected a request whose query matches the signed envelope to authenticate")
    }
}

func TestHmacTokenSource_QueryTamperingIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, _ := signer.Sign("GET", "/internal/orders?status=open", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/orders?status=all", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a tampered query string to be rejected")
    }
}

func TestHmacTokenSource_RejectsEnvelopeTooCloseToExpiryForReplayGuard(t *testing.T) {
    source := hmacTestSource(NewMemoryNonceGuard())

    envelope := hmacEnvelope{Nonce: "n-1", ExpiresAt: time.Now().Add(-time.Second).Unix()}

    if guardErr := source.guardNonce(testRuntime(), "key-current", envelope); nil == guardErr {
        t.Fatal("expected an envelope past its guardable window to be rejected")
    }
}

func TestHmacTokenSource_NonceIsNamespacedAwayFromTotpGuard(t *testing.T) {
    guard := NewMemoryNonceGuard()
    source := NewHmacTokenSource(HmacTokenSourceConfig{Secrets: hmacTestSecrets(), Apps: hmacTestApps(), NonceGuard: guard})

    totpGuardKey := "2fa:alice:000000"

    forged := craftHmacHeaderValueWithNonce(t, "key-current", []byte("current-shared-secret-value-0001"), "wms-service", "GET", "/internal/ping", nil, totpGuardKey)

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, forged))
    if false == token.IsAuthenticated() {
        t.Fatal("expected the crafted envelope to authenticate: it carries a valid key bound to the claimed app")
    }

    seen, rememberErr := guard.Remember(testRuntime(), totpGuardKey, time.Minute)
    if nil != rememberErr {
        t.Fatalf("remember: %v", rememberErr)
    }

    if true == seen {
        t.Fatal("expected the TOTP guard key to be unseen: the HMAC nonce must be namespaced away from the 2fa key space")
    }
}

func TestHmacTokenSource_AcceptsPreviousActiveKey(t *testing.T) {
    previousSigner := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{
        App:     "wms-service",
        Secrets: NewStaticHmacSecretProvider("key-previous", map[string]HmacKey{"key-previous": {App: "wms-service", Secret: []byte("previous-shared-secret-value-002")}}),
    })
    headerValue, _ := previousSigner.Sign("GET", "/internal/ping", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, previousSigner.HeaderName(), headerValue))

    if false == token.IsAuthenticated() {
        t.Fatal("expected a previous-but-active key to verify")
    }
}

func TestHmacTokenSource_RejectsUnknownKeyId(t *testing.T) {
    unknownSigner := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{
        App:     "wms-service",
        Secrets: NewStaticHmacSecretProvider("key-retired", map[string]HmacKey{"key-retired": {App: "wms-service", Secret: []byte("retired-shared-secret-value-0003")}}),
    })
    headerValue, _ := unknownSigner.Sign("GET", "/internal/ping", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, unknownSigner.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope signed with an unknown key id to be rejected")
    }
}

func TestHmacTokenSource_MissingHeaderIsAnonymousWithoutError(t *testing.T) {
    source := hmacTestSource(NewMemoryNonceGuard())
    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, ""))

    if nil != resolveErr {
        t.Fatalf("expected no error for a missing header, got %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatal("expected a missing header to resolve to anonymous")
    }
}

func TestHmacTokenSource_RestoresBodyForDownstreamHandler(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign("POST", "/internal/orders", body, nil)

    request := hmacRequest("POST", "/internal/orders", body, signer.HeaderName(), headerValue)

    source := hmacTestSource(NewMemoryNonceGuard())
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
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets(), Ttl: time.Hour})
    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:         hmacTestSecrets(),
        Apps:            hmacTestApps(),
        NonceGuard:      NewMemoryNonceGuard(),
        MaxFutureExpiry: time.Minute,
    })
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope expiring beyond the max future horizon to be rejected")
    }
}

func TestHmacTokenSource_AcceptsLongLivedEnvelopeWhenHorizonUnbounded(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets(), Ttl: time.Hour})
    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))

    if false == token.IsAuthenticated() {
        t.Fatal("expected a long-lived envelope to authenticate when the horizon is unbounded")
    }
}

func TestHmacTokenSource_BodyBeforeNonceSurvivesNonceBurn(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign("POST", "/internal/orders", body, nil)

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:               hmacTestSecrets(),
        Apps:                  hmacTestApps(),
        NonceGuard:            NewMemoryNonceGuard(),
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
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign("POST", "/internal/orders", body, nil)

    source := hmacTestSource(NewMemoryNonceGuard())

    source.Resolve(testRuntime(), hmacRequest("POST", "/internal/orders", []byte(`{"sku":"X-999"}`), signer.HeaderName(), headerValue))

    genuine, _ := source.Resolve(testRuntime(), hmacRequest("POST", "/internal/orders", body, signer.HeaderName(), headerValue))
    if true == genuine.IsAuthenticated() {
        t.Fatal("expected the genuine request to be denied after the nonce was burned by a mutated-body replay")
    }
}

func TestHmacTokenSource_PerRequestBodyBeforeNonceOverride(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    body := []byte(`{"sku":"X-1"}`)
    headerValue, _ := signer.Sign("POST", "/internal/orders", body, nil)

    source := hmacTestSource(NewMemoryNonceGuard())

    burnRequest := hmacRequest("POST", "/internal/orders", []byte(`{"sku":"X-999"}`), signer.HeaderName(), headerValue)
    SetHmacVerifyBodyBeforeNonce(burnRequest, true)
    burn, _ := source.Resolve(testRuntime(), burnRequest)
    if true == burn.IsAuthenticated() {
        t.Fatal("expected the mutated-body replay to be rejected under the per-request override")
    }

    genuineRequest := hmacRequest("POST", "/internal/orders", body, signer.HeaderName(), headerValue)
    SetHmacVerifyBodyBeforeNonce(genuineRequest, true)
    genuine, _ := source.Resolve(testRuntime(), genuineRequest)
    if false == genuine.IsAuthenticated() {
        t.Fatal("expected the genuine request to authenticate under the per-request override")
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
    firewall := NewCompiledFirewall(
        "internal",
        &resolutionListenerTestMatcher{matches: true},
        "matcher:internal",
        []securitycontract.Rule{},
        source,
        NewAccessControl(),
        NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            NewRoleHierarchyVoter(NewRoleHierarchy(map[string][]string{}), NewRoleVoter()),
        ),
        NewRoleHierarchy(map[string][]string{}),
        nil,
        nil,
        "/login",
        "/logout",
        nil,
        nil,
        SourceFirewall,
        SourceFirewall,
        SourceFirewall,
        SourceNone,
        SourceNone,
    )

    return NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil))
}

func TestHmacTokenSource_EndToEndResolvesServiceWithActorThroughFirewall(t *testing.T) {
    secrets := NewStaticHmacSecretProvider("k1", map[string]HmacKey{"k1": {App: "wms-service", Secret: []byte("hmac-e2e-shared-secret-value")}})
    apps := NewStaticHmacAppRegistry(map[string][]string{"wms-service": {"ROLE_SERVICE"}})
    source := NewHmacTokenSource(HmacTokenSourceConfig{Secrets: secrets, Apps: apps, NonceGuard: NewMemoryNonceGuard()})

    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: secrets})
    actor := NewActor("user-7", securitycontract.ActorTypeUser, []string{"ROLE_BUYER"}, map[string]string{"tenant": "acme"})

    headerValue, signErr := signer.Sign("GET", "/internal/ping", nil, actor)
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    kernel := newTestKernel()
    registerTestKernelExceptionListener(kernel)
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

    guard := NewMemoryNonceGuard()

    firstSeen, firstErr := guard.Remember(testRuntime(), hmacNonceGuardKey("a", "b:nonce"), time.Minute)
    if nil != firstErr || true == firstSeen {
        t.Fatalf("expected the first nonce to store, got seen=%t err=%v", firstSeen, firstErr)
    }

    secondSeen, secondErr := guard.Remember(testRuntime(), hmacNonceGuardKey("a:b", "nonce"), time.Minute)
    if nil != secondErr || true == secondSeen {
        t.Fatalf("expected key a:b's nonce to stay unburned, got seen=%t err=%v", secondSeen, secondErr)
    }
}

func TestHmacTokenSource_TheMigrationWindowAuthenticatesAnEnvelopeWithoutTheInternalAuthType(t *testing.T) {
    secret := []byte("current-shared-secret-value-0001")
    now := time.Now()

    payloadBytes, _ := json.Marshal(hmacEnvelope{
        App:       "wms-service",
        Method:    "GET",
        Path:      "/internal/ping",
        IssuedAt:  now.Unix(),
        ExpiresAt: now.Add(30 * time.Second).Unix(),
        Nonce:     "nonce-window-1",
        BodyHash:  hashBody(nil),
    })

    headerBytes, _ := json.Marshal(map[string]string{"alg": "HS256", "kid": "key-current"})

    part0 := base64.RawURLEncoding.EncodeToString(headerBytes)
    part1 := base64.RawURLEncoding.EncodeToString(payloadBytes)
    signature := signHmacSha256(part0+"."+part1, secret)
    headerValue := part0 + "." + part1 + "." + base64.RawURLEncoding.EncodeToString(signature)

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:                hmacTestSecrets(),
        Apps:                   hmacTestApps(),
        NonceGuard:             NewMemoryNonceGuard(),
        AcceptUntypedEnvelopes: true,
    })

    token, resolveErr := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatal("expected the migration window to authenticate an envelope minted before the typ was emitted")
    }
}

func TestHmacTokenSource_RejectsAnEnvelopeWithoutTheInternalAuthType(t *testing.T) {
    secret := []byte("current-shared-secret-value-0001")
    now := time.Now()

    payloadBytes, _ := json.Marshal(hmacEnvelope{
        App:       "wms-service",
        Method:    "GET",
        Path:      "/internal/ping",
        IssuedAt:  now.Unix(),
        ExpiresAt: now.Add(30 * time.Second).Unix(),
        Nonce:     "nonce-1",
        BodyHash:  hashBody(nil),
    })

    headerBytes, _ := json.Marshal(map[string]string{"alg": "HS256", "kid": "key-current"})

    part0 := base64.RawURLEncoding.EncodeToString(headerBytes)
    part1 := base64.RawURLEncoding.EncodeToString(payloadBytes)
    signature := signHmacSha256(part0+"."+part1, secret)
    headerValue := part0 + "." + part1 + "." + base64.RawURLEncoding.EncodeToString(signature)

    source := hmacTestSource(NewMemoryNonceGuard())
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

func (instance *failingNonceGuard) Remember(_ runtimecontract.Runtime, _ string, _ time.Duration) (bool, error) {
    return false, instance.failure
}

func TestHmacTokenSource_NonceGuardFailureLogsAtErrorAndFailsClosed(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, signErr := signer.Sign("GET", "/internal/ping", nil, nil)
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := hmacTestSource(&failingNonceGuard{failure: errors.New("redis is down")})
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, hmacRequest("GET", "/internal/ping", nil, signer.HeaderName(), headerValue))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatal("an unanswerable nonce guard must fail closed to anonymous")
    }

    errorRecords := logger.recordsAtLevel(loggingcontract.LevelError)
    if 1 != len(errorRecords) {
        t.Fatalf("the guard's backend being down degrades every internal caller to anonymous at once and must be filed at Error, got %d error records", len(errorRecords))
    }

    if 0 != len(logger.recordsAtLevel(loggingcontract.LevelInfo)) {
        t.Fatal("the infrastructure failure must not additionally be filed as a routine Info rejection")
    }
}

func TestHmacTokenSource_ForgedEnvelopeStaysAtInfo(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, _ := signer.Sign("GET", "/internal/ping", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())
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
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, signErr := signer.Sign("GET", "/internal/ping?api_key=SIGNEDSECRETVALUE", nil, nil)
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := hmacTestSource(NewMemoryNonceGuard())
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

func TestHmacTokenSource_TimeWindowRunsOnTheInjectedClock(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))

    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets(), Clock: frozen})
    headerValue, signErr := signer.Sign("GET", "/internal/ping", nil, nil)
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets: hmacTestSecrets(),
        Apps:    hmacTestApps(),
        Clock:   frozen,
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

func TestNewHmacTokenSource_NegativeMaxFutureExpiryPanics(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a negative max future expiry to be refused at construction")
        }
    }()

    _ = NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:         hmacTestSecrets(),
        Apps:            hmacTestApps(),
        MaxFutureExpiry: -1 * time.Minute,
    })
}

func TestHmacTokenSource_TheTimeWindowAdmitsTheDeadlineTheNonceGuardRefuses(t *testing.T) {
    now := time.Unix(1_700_000_000, 0)
    frozen := clock.NewFrozenClock(now)

    source := NewHmacTokenSource(HmacTokenSourceConfig{
        Secrets:    hmacTestSecrets(),
        Apps:       hmacTestApps(),
        NonceGuard: NewMemoryNonceGuardWithClock(frozen),
        Clock:      frozen,
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

func TestHmacTokenSource_AnEnvelopePastTheDeadlineIsRefusedByTheTimeWindow(t *testing.T) {
    source := hmacTestSource(NewMemoryNonceGuard())

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
    source := hmacTestSource(NewMemoryNonceGuard())

    now := time.Unix(1_700_000_000, 0)
    envelope := hmacEnvelope{
        IssuedAt:  now.Add(time.Hour).Unix(),
        ExpiresAt: now.Add(2 * time.Hour).Unix(),
        Nonce:     "n1",
    }

    timeErr := source.verifyTimeWindow(envelope, now)
    if nil == timeErr {
        t.Fatalf("expected an envelope issued an hour in the future to be refused")
    }

    if "internal-auth envelope is not yet valid" != timeErr.Error() {
        t.Fatalf("unexpected refusal message: %q", timeErr.Error())
    }
}
