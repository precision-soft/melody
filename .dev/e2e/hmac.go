package main

import (
    "strconv"
    "time"

    rueidisintegration "github.com/precision-soft/melody/integrations/rueidis/v3"
    security "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* runHmacCheck exercises the full F1/F2 cross-app machine-to-machine auth flow against a REAL redis-backed
nonce guard — the multi-instance replay story an in-process guard cannot demonstrate. A calling service
(wms) signs an actor-carrying envelope for a named audience (erp); the callee (erp) verifies it through a
rueidis NonceGuard, propagates the originating actor, then proves the same envelope is refused on replay,
on audience mismatch, and on a tampered body. */
func runHmacCheck(address string) {
    provider := rueidisintegration.NewProvider()
    client, openErr := provider.Open(rueidisintegration.NewConnectionParameters(address, "", ""))
    if nil != openErr {
        fail("open redis client: %v", openErr)
    }
    defer client.Close()

    /* a per-run key namespace so a nonce recorded by an earlier run never masks this run's first use */
    noncePrefix := "melody:nonce:e2e:" + strconv.FormatInt(time.Now().UnixNano(), 10)

    secrets := security.NewStaticHmacSecretProvider(
        "wms-key-1",
        map[string]security.HmacKey{
            "wms-key-1": {App: "wms-service", Secret: []byte("wms-shared-secret-value-0000001")},
            "erp-key-1": {App: "erp-service", Secret: []byte("erp-shared-secret-value-0000001")},
        },
    )

    apps := security.NewStaticHmacAppRegistry(map[string][]string{
        "wms-service": {"ROLE_SERVICE", "ROLE_WMS"},
        "erp-service": {"ROLE_SERVICE", "ROLE_ERP"},
    })

    erpSource := security.NewHmacTokenSource(security.HmacTokenSourceConfig{
        Secrets:         secrets,
        Apps:            apps,
        NonceGuard:      rueidisintegration.NewNonceGuardWithPrefix(client, noncePrefix),
        ServiceIdentity: "erp-service",
    })

    wmsSigner := security.NewHmacEnvelopeSigner(security.HmacEnvelopeSignerConfig{
        App:      "wms-service",
        Secrets:  secrets,
        Audience: "erp-service",
    })

    actor := security.NewActor(
        "user-42",
        securitycontract.ActorTypeUser,
        []string{"ROLE_BUYER"},
        map[string]string{"tenant": "acme"},
    )
    body := []byte(`{"order":"SO-1001","sku":"WIDGET-9"}`)

    header := mustSign(wmsSigner, "POST", "/internal/orders", body, actor)

    /* (1) happy path: authenticates as the wms SERVICE principal, carries the registry roles, and
       propagates the originating human actor so erp can authorize/audit on its behalf */
    token := resolveHmac(erpSource, "POST", "/internal/orders", body, wmsSigner.HeaderName(), header)
    if false == token.IsAuthenticated() {
        fail("(1) expected the cross-app envelope to authenticate")
    }
    if "wms-service" != token.UserIdentifier() {
        fail("(1) expected principal wms-service, got %q", token.UserIdentifier())
    }
    actorAware, isAware := token.(securitycontract.ActorAware)
    if false == isAware {
        fail("(1) expected the token to be actor-aware")
    }
    onBehalf, present := actorAware.OnBehalfOf()
    if false == present || "user-42" != onBehalf.Identifier() {
        fail("(1) expected originating actor user-42, present=%v", present)
    }
    pass("(1) authenticated as %q roles=%v on-behalf-of=%q tenant=%q",
        token.UserIdentifier(), token.Roles(), onBehalf.Identifier(), onBehalf.Attributes()["tenant"])

    /* (2) replay: the SAME header verified again must be refused — the live redis guard remembered the
       nonce, so even a different erp instance sharing the guard rejects the captured envelope */
    replayToken := resolveHmac(erpSource, "POST", "/internal/orders", body, wmsSigner.HeaderName(), header)
    if true == replayToken.IsAuthenticated() {
        fail("(2) expected the replayed envelope to be refused by the live redis nonce guard")
    }
    pass("(2) replayed envelope refused → authenticated=%v (redis nonce consumed)", replayToken.IsAuthenticated())

    /* (3) audience binding: an envelope minted for erp presented to a DIFFERENT service (billing) is
       refused because its ServiceIdentity does not match the signed audience */
    billingSource := security.NewHmacTokenSource(security.HmacTokenSourceConfig{
        Secrets:         secrets,
        Apps:            apps,
        NonceGuard:      rueidisintegration.NewNonceGuardWithPrefix(client, noncePrefix+":billing"),
        ServiceIdentity: "billing-service",
    })
    audienceToken := resolveHmac(billingSource, "POST", "/internal/orders", body, wmsSigner.HeaderName(),
        mustSign(wmsSigner, "POST", "/internal/orders", body, actor))
    if true == audienceToken.IsAuthenticated() {
        fail("(3) expected an envelope minted for erp to be refused by billing (audience mismatch)")
    }
    pass("(3) erp-audience envelope refused by billing-service → authenticated=%v", audienceToken.IsAuthenticated())

    /* (4) body tamper: a fresh envelope whose body is altered in flight fails the body-hash bind */
    tamperToken := resolveHmac(erpSource, "POST", "/internal/orders", []byte(`{"order":"SO-9999","sku":"HACKED"}`),
        wmsSigner.HeaderName(), mustSign(wmsSigner, "POST", "/internal/orders", body, actor))
    if true == tamperToken.IsAuthenticated() {
        fail("(4) expected a tampered body to be refused (body hash mismatch)")
    }
    pass("(4) tampered body refused → authenticated=%v", tamperToken.IsAuthenticated())
}

func mustSign(signer *security.HmacEnvelopeSigner, method string, path string, body []byte, actor securitycontract.Actor) string {
    header, signErr := signer.Sign(method, path, body, actor)
    if nil != signErr {
        fail("sign: %v", signErr)
    }

    return header
}

func resolveHmac(source *security.HmacTokenSource, method string, path string, body []byte, headerName string, headerValue string) securitycontract.Token {
    token, resolveErr := source.Resolve(newRuntime(), buildRequest(method, path, body, headerName, headerValue))
    if nil != resolveErr {
        fail("resolve: %v", resolveErr)
    }

    return token
}
