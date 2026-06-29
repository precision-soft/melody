package security

import (
    "bytes"
    "io"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
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

/* negative control: a tampered envelope must not authenticate. */
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
    /* the signer's key is legitimately bound to ghost-service and it claims ghost-service, so the key-id↔app binding passes; the verifier knows that key id too, but its app registry does not list ghost-service, so no roles can be granted and the envelope resolves to anonymous. This isolates the unknown-app branch, which now runs after the binding check. */
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

/* the key-id↔app binding is only sound when each app's secret material is distinct, so the provider refuses the same secret bytes under key ids belonging to different apps — otherwise a holder could sign under a sibling app's key id and defeat the binding. */
func TestStaticHmacSecretProvider_RejectsSecretReusedAcrossApps(t *testing.T) {
    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatal("expected a panic when the same secret is registered for two different apps")
        }
    }()

    shared := []byte("shared-secret-value-across-apps-1")
    NewStaticHmacSecretProvider("key-a", map[string]HmacKey{
        "key-a": {App: "app-a", Secret: shared},
        "key-b": {App: "app-b", Secret: shared},
    })
}

/* positive control: the same app may legitimately own several key ids (rotation overlap), even reusing material is fine within one app — only cross-app reuse is rejected. */
func TestStaticHmacSecretProvider_AllowsMultipleKeysForOneApp(t *testing.T) {
    provider := NewStaticHmacSecretProvider("key-current", map[string]HmacKey{
        "key-current":  {App: "wms-service", Secret: []byte("current-shared-secret-value-0001")},
        "key-previous": {App: "wms-service", Secret: []byte("previous-shared-secret-value-002")},
    })

    if app, bound := provider.AppForKeyId("key-previous"); false == bound || "wms-service" != app {
        t.Fatalf("expected key-previous bound to wms-service, got %q bound=%v", app, bound)
    }
}

/* negative control: the strongest internal-auth threat. An attacker who holds a valid shared secret (here the secret issued to key-current / wms-service) signs an envelope claiming a different, higher-privileged app. The signature verifies and the claimed app is registered, but the key id is bound to wms-service, not admin-service, so the verifier refuses it. This is what closes the cross-app privilege-escalation vector: a secret is only ever as privileged as the single app its key id is issued to. */
func TestHmacTokenSource_CrossAppClaimWithValidKeyIsAnonymous(t *testing.T) {
    /* the verifier knows both apps, so a rejection can only come from the key-id↔app binding, not from an unknown app */
    apps := NewStaticHmacAppRegistry(map[string][]string{
        "wms-service":   {"ROLE_SERVICE"},
        "admin-service": {"ROLE_ADMIN"},
    })
    source := NewHmacTokenSource(HmacTokenSourceConfig{Secrets: hmacTestSecrets(), Apps: apps, NonceGuard: NewMemoryNonceGuard()})

    /* forge the envelope directly: the real signer refuses to sign for an app its key is not bound to, but an attacker would not use it. key-current's secret is bound to wms-service in the verifier. */
    forged := craftHmacHeaderValue(t, "key-current", []byte("current-shared-secret-value-0001"), "admin-service", "GET", "/internal/ping", nil)

    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/ping", nil, DefaultHmacHeaderName, forged))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an envelope claiming an app its key id is not bound to be rejected")
    }
}

/* craftHmacHeaderValue builds a fully valid signed envelope for an arbitrary key id and claimed app, bypassing the signer's own app-binding guard — it models an attacker who holds a secret and signs whatever app it likes. */
func craftHmacHeaderValue(t *testing.T, keyId string, secret []byte, app string, method string, path string, body []byte) string {
    t.Helper()

    nonce, nonceErr := newNonce()
    if nil != nonceErr {
        t.Fatalf("nonce: %v", nonceErr)
    }

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

    /* same envelope replayed against a different path */
    wrongPath, _ := source.Resolve(testRuntime(), hmacRequest("POST", "/internal/refunds", nil, signer.HeaderName(), headerValue))
    if true == wrongPath.IsAuthenticated() {
        t.Fatal("expected a path mismatch to be rejected")
    }

    /* and against a different method */
    wrongMethod, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/orders", nil, signer.HeaderName(), headerValue))
    if true == wrongMethod.IsAuthenticated() {
        t.Fatal("expected a method mismatch to be rejected")
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

/* negative control: the query string is covered by the signature, so tampering with it (as the body and path already are) is rejected. */
func TestHmacTokenSource_QueryTamperingIsAnonymous(t *testing.T) {
    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, _ := signer.Sign("GET", "/internal/orders?status=open", nil, nil)

    source := hmacTestSource(NewMemoryNonceGuard())
    token, _ := source.Resolve(testRuntime(), hmacRequest("GET", "/internal/orders?status=all", nil, signer.HeaderName(), headerValue))

    if true == token.IsAuthenticated() {
        t.Fatal("expected a tampered query string to be rejected")
    }
}

/* negative control: an envelope whose acceptance window has lapsed yields a non-positive guard ttl that the nonce guard would not record; admitting it would leave it replayable, so guardNonce rejects it rather than accept-without-recording. */
func TestHmacTokenSource_RejectsEnvelopeTooCloseToExpiryForReplayGuard(t *testing.T) {
    source := hmacTestSource(NewMemoryNonceGuard())

    envelope := hmacEnvelope{Nonce: "n-1", ExpiresAt: time.Now().Add(-time.Second).Unix()}

    if guardErr := source.guardNonce(testRuntime(), envelope); nil == guardErr {
        t.Fatal("expected an envelope past its guardable window to be rejected")
    }
}

func TestHmacTokenSource_AcceptsPreviousActiveKey(t *testing.T) {
    /* a signer pinned to the previous key id still verifies while that key stays active (rotation overlap) */
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

/* tamperHmacPayload flips a character in the signed payload (the middle base64 segment) so the HMAC over `header.payload` no longer matches the signature — a reliable corruption, unlike flipping the signature's trailing base64 character whose low bits are not significant and can decode to the same bytes. */
func tamperHmacPayload(headerValue string) string {
    parts := strings.SplitN(headerValue, ".", 3)

    replacement := byte('A')
    if 'A' == parts[1][0] {
        replacement = 'B'
    }

    parts[1] = string(replacement) + parts[1][1:]

    return parts[0] + "." + parts[1] + "." + parts[2]
}

/* hmacE2EFirewall wires a real HmacTokenSource into a compiled firewall + the kernel security
   resolution listener, exercising the exact path a product uses (not Resolve in isolation). */
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

    /* first dispatch: the signed envelope resolves to the service principal carrying the actor */
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

    /* second dispatch on a fresh runtime: the SAME envelope is a replay and must not authenticate */
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
