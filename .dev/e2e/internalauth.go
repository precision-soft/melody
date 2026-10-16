package main

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"
)

const internalAuthLabel = "internal auth over http"

/* the stateless HMAC firewall guards /internal (config/security.go) and /internal/whoami/ echoes the service principal it authenticated together with the actor the caller propagated (handler/internalauth/whoami_handler.go). The route is POST-only and the signed method/path must match the request exactly, so every call below uses POST and the same path the envelope was minted for. */
const internalAuthRoute = "/internal/whoami/"

/* internalAuthHeaderName is DefaultHmacHeaderName; internal:sign prints it above the envelope, and the section reads that printed name rather than assuming it, so a rename in the framework surfaces here as a mismatch instead of as a silent 401. */
const internalAuthHeaderName = "X-Melody-Internal-Auth"

/* internalAuthPayload mirrors handler/internalauth.whoamiPayload. */
type internalAuthPayload struct {
    ServicePrincipal string   `json:"servicePrincipal"`
    Roles            []string `json:"roles"`
    OnBehalfOf       *struct {
        Identifier string            `json:"identifier"`
        Type       string            `json:"type"`
        Roles      []string          `json:"roles"`
        Attributes map[string]string `json:"attributes"`
    } `json:"onBehalfOf,omitempty"`
}

/* runInternalAuthCheck exercises the cross-app HMAC scheme ACROSS A REAL SERVICE BOUNDARY: the envelope is minted by the example's internal:sign command in one process and verified by the running application in another, over http, through the whole request pipeline. The in-process hmac.go section stays exactly as it is — it drives the token source directly against a live redis nonce guard. This route's firewall carries the shared redis guard too when the example wires redis, and the accepted envelope's nonce is read back out of band under the example's prefix: the replay refusal alone cannot tell the shared guard from the in-process one, since the development stack runs one process.

   Every negative mints its OWN envelope. Reusing one would make each negative pass for the wrong reason: the second presentation of any envelope is refused as a replay, so a broken signature check would still look correct. */
func runInternalAuthCheck(baseUrl string, redisAddress string) {
    client := newLiveExampleClient(baseUrl)

    assertInternalAuthAcceptedAndReplayed(client, redisAddress)
    assertInternalAuthRejected(client, "no internal-auth header at all", internalAuthRoute, nil, "")
    /* the machine firewall carries its own rule for every path under /internal: an unsigned call to a path no route serves is refused by the firewall's entry point, not answered 404 by routing it would reach if no rule claimed it */
    assertInternalAuthRejected(client, "an unsigned call to an unrouted path under /internal", "/internal/not-routed/", nil, "")
    assertInternalAuthTamperedSignature(client)
    assertInternalAuthBodyMismatch(client)
    assertInternalAuthQueryMismatch(client)
    assertInternalAuthRefusesAnEnvelopePastTheHorizon(client)
}

/* assertInternalAuthAcceptedAndReplayed covers the positive and the replay in one place because they must share one envelope: the replay negative is only meaningful for an envelope that was ACCEPTED first. */
func assertInternalAuthAcceptedAndReplayed(client *liveExampleClient, redisAddress string) {
    envelope, elapsed := mintInternalAuthEnvelope("--actor", "user-42")

    response := client.call(internalAuthLabel, liveExampleRequest{
        method:     "POST",
        path:       internalAuthRoute,
        headerList: map[string]string{internalAuthHeaderName: envelope},
    })

    if http.StatusUnauthorized == response.statusCode {
        fail(
            "%s: the envelope the application's own internal:sign command minted was refused with 401%s",
            internalAuthLabel,
            exampleMintDelayDiagnostic(elapsed),
        )
    }
    requireLiveExampleStatus(internalAuthLabel, internalAuthRoute+" with a signed envelope", response, http.StatusOK)

    payload := internalAuthPayload{}
    decodeLiveExamplePayload(internalAuthLabel, response, &payload)

    if "wms-service" != payload.ServicePrincipal {
        fail(
            "%s: the request authenticated as %q, wanted the calling service principal %q",
            internalAuthLabel,
            payload.ServicePrincipal,
            "wms-service",
        )
    }
    if false == tokenAuthHasRole(payload.Roles, "ROLE_SERVICE") {
        fail(
            "%s: the service principal holds roles %v, wanted the app registry's ROLE_SERVICE",
            internalAuthLabel,
            payload.Roles,
        )
    }

    /* the propagated actor is the whole point of the scheme: without it the callee knows only which SERVICE called and can neither authorize nor audit on behalf of the human who started the chain */
    if nil == payload.OnBehalfOf {
        fail(
            "%s: the request authenticated as %q but carried NO originating actor — the human behind the call was lost crossing the service boundary",
            internalAuthLabel,
            payload.ServicePrincipal,
        )
    }
    if "user-42" != payload.OnBehalfOf.Identifier {
        fail(
            "%s: the propagated actor is %q, wanted the signed %q",
            internalAuthLabel,
            payload.OnBehalfOf.Identifier,
            "user-42",
        )
    }
    if "acme" != payload.OnBehalfOf.Attributes["tenant"] {
        fail(
            "%s: the propagated actor's tenant attribute is %q, wanted %q — the actor arrived stripped of the attributes it was signed with",
            internalAuthLabel,
            payload.OnBehalfOf.Attributes["tenant"],
            "acme",
        )
    }

    pass(
        "a signed envelope authenticated as %q (roles %v) on behalf of %q (tenant %q) across a real process boundary",
        payload.ServicePrincipal,
        payload.Roles,
        payload.OnBehalfOf.Identifier,
        payload.OnBehalfOf.Attributes["tenant"],
    )

    /* the same envelope presented a second time must be refused: its nonce was consumed by the accepted call above, so a captured header on the wire buys an attacker exactly one nothing */
    replay := client.call(internalAuthLabel, liveExampleRequest{
        method:     "POST",
        path:       internalAuthRoute,
        headerList: map[string]string{internalAuthHeaderName: envelope},
    })

    if http.StatusUnauthorized != replay.statusCode {
        fail(
            "%s: the SAME envelope was accepted a second time (%d) — a captured header can be replayed for the rest of its lifetime: %s",
            internalAuthLabel,
            replay.statusCode,
            exampleTruncate(replay.bodyText()),
        )
    }

    pass("the same envelope presented a second time was refused with 401 (its nonce was consumed)")

    assertInternalAuthNonceSharedInRedis(envelope, redisAddress)

    /* a stale-envelope negative is deliberately NOT asserted here: the envelope's lifetime is exampleHmacEnvelopeLifetime (30s) and internal:sign has no flag to backdate one, so the only way to observe expiry over http would be to sleep 31 seconds inside the run. The expiry check is covered by the framework's own unit tests, which can move the clock. */
}

func assertInternalAuthTamperedSignature(client *liveExampleClient) {
    envelope, _ := mintInternalAuthEnvelope()

    tampered := tokenAuthTamperedSignature(envelope)
    if tampered == envelope {
        fail("%s: the signature tamper produced the SAME envelope, so the negative below would prove nothing", internalAuthLabel)
    }

    assertInternalAuthRejected(client, "an envelope whose signature was flipped", internalAuthRoute, nil, tampered)
}

/* assertInternalAuthBodyMismatch signs one body and sends a different one. The envelope carries a hash of the body it was minted for, so this is the assertion that a valid envelope cannot be lifted onto a rewritten request — the exact attack a man-in-the-middle attempts against a signed api call. */
func assertInternalAuthBodyMismatch(client *liveExampleClient) {
    envelope, _ := mintInternalAuthEnvelope("--body", `{"order":"SO-1001"}`)

    assertInternalAuthRejected(
        client,
        "an envelope signed for a different body than the one sent",
        internalAuthRoute,
        []byte(`{"order":"SO-9999"}`),
        envelope,
    )
}

/* assertInternalAuthQueryMismatch signs a query string and sends another. The query is signed separately from the path, so an envelope minted for ?tenant=acme must not authorize ?tenant=victim — without this bind, a signed call can be redirected onto another tenant's data while every other check still passes. */
func assertInternalAuthQueryMismatch(client *liveExampleClient) {
    envelope, _ := mintInternalAuthEnvelope("--path", internalAuthRoute+"?tenant=acme")

    assertInternalAuthRejected(
        client,
        "an envelope signed for ?tenant=acme presented on ?tenant=victim",
        internalAuthRoute+"?tenant=victim",
        nil,
        envelope,
    )
}

func assertInternalAuthRejected(client *liveExampleClient, what string, path string, body []byte, envelope string) {
    headerList := map[string]string{}
    if "" != envelope {
        headerList[internalAuthHeaderName] = envelope
    }

    response := client.call(internalAuthLabel, liveExampleRequest{
        method:      "POST",
        path:        path,
        contentType: "application/json",
        headerList:  headerList,
        body:        body,
    })

    if http.StatusUnauthorized != response.statusCode {
        fail(
            "%s: %s was answered %d, wanted 401: %s",
            internalAuthLabel,
            what,
            response.statusCode,
            exampleTruncate(response.bodyText()),
        )
    }

    pass("%s was refused with 401", what)
}

/* mintInternalAuthEnvelope runs internal:sign and returns the envelope together with how long the mint took. The default path the command signs is internalAuthRoute, so a caller that passes no --path gets an envelope bound to the route the section calls. The printed HEADER NAME is verified against the constant here rather than ignored: the command prints it precisely so a client does not have to guess, and a divergence would otherwise surface as an unexplained 401 in every assertion below. */
func mintInternalAuthEnvelope(arguments ...string) (string, time.Duration) {
    output, elapsed := runExampleMintCommand(internalAuthLabel, append([]string{"internal:sign"}, arguments...)...)

    if false == internalAuthOutputNamesHeader(output) {
        fail(
            "%s: internal:sign did not print the %s header name, so the harness cannot know which header to send:\n%s",
            internalAuthLabel,
            internalAuthHeaderName,
            exampleTail(output, 20),
        )
    }

    return exampleMintedToken(internalAuthLabel, output), elapsed
}

func internalAuthOutputNamesHeader(output string) bool {
    for _, line := range strings.Split(output, "\n") {
        if internalAuthHeaderName == strings.TrimSpace(line) {
            return true
        }
    }

    return false
}

/* the firewall bounds how far ahead an envelope's expiry may sit (MaxFutureExpiry, five minutes), since its nonce guard remembers each nonce until its envelope expires: an envelope minted for an hour is refused, and one minted for four minutes, inside the horizon, is accepted — the control that the refusal is the horizon's and not the --ttl flag's */
func assertInternalAuthRefusesAnEnvelopePastTheHorizon(client *liveExampleClient) {
    call := func(envelope string) int {
        return client.call(internalAuthLabel, liveExampleRequest{
            method:     "POST",
            path:       internalAuthRoute,
            headerList: map[string]string{internalAuthHeaderName: envelope},
        }).statusCode
    }

    insideEnvelope, insideElapsed := mintInternalAuthEnvelope("--ttl", "4m")
    if status := call(insideEnvelope); http.StatusOK != status {
        fail("%s: an envelope minted for four minutes answered %d, wanted 200 inside the five-minute horizon%s", internalAuthLabel, status, exampleMintDelayDiagnostic(insideElapsed))
    }

    pastEnvelope, _ := mintInternalAuthEnvelope("--ttl", "1h")
    if status := call(pastEnvelope); http.StatusUnauthorized != status {
        fail("%s: an envelope minted for an hour answered %d, wanted 401 past the five-minute horizon", internalAuthLabel, status)
    }

    pass("an envelope minted for four minutes is accepted and one minted for an hour is refused 401: the firewall bounds the expiry horizon")
}

/* internalAuthNoncePrefix is the key prefix the example's shared nonce guard remembers accepted nonces under (config/security.go) */
const internalAuthNoncePrefix = "melody-example-v3:nonce:"

/* the accepted envelope's nonce is what separates the shared guard from the in-process one: it is read out of the envelope's signed payload and must exist in redis under the example's prefix, remembered at most as long as the envelope lives */
func assertInternalAuthNonceSharedInRedis(envelope string, redisAddress string) {
    if "" == redisAddress {
        fmt.Println("SKIPPED: the shared nonce guard needs REDIS_ADDRESS; without redis the example keeps the in-process guard")

        return
    }

    partList := strings.Split(envelope, ".")
    if 3 != len(partList) {
        fail("%s: the minted envelope is not three dotted parts: %s", internalAuthLabel, envelope)
    }

    payloadBytes, decodeErr := base64.RawURLEncoding.DecodeString(partList[1])
    if nil != decodeErr {
        fail("%s: the envelope's payload is not base64url: %v", internalAuthLabel, decodeErr)
    }

    payload := struct {
        Nonce     string `json:"nonce"`
        ExpiresAt int64  `json:"exp"`
    }{}
    if unmarshalErr := json.Unmarshal(payloadBytes, &payload); nil != unmarshalErr || "" == payload.Nonce {
        fail("%s: the envelope's payload carries no nonce: %v (%s)", internalAuthLabel, unmarshalErr, payloadBytes)
    }

    redisClient := openRedis(redisAddress)
    defer redisClient.Close()

    /* the token source namespaces a nonce by its scheme and key id before the guard's prefix, so the key is found by the example's prefix and the nonce rather than spelled from that layout */
    keyList, keysErr := redisClient.Do(context.Background(), redisClient.B().Keys().Pattern(internalAuthNoncePrefix+"*"+payload.Nonce).Build()).AsStrSlice()
    if nil != keysErr || 1 != len(keyList) {
        fail("%s: the accepted nonce %s is remembered under %d keys below %s (%v), wanted exactly one — the firewall is not on the shared guard", internalAuthLabel, payload.Nonce, len(keyList), internalAuthNoncePrefix, keysErr)
    }

    remainingMilliseconds, ttlErr := redisClient.Do(context.Background(), redisClient.B().Pttl().Key(keyList[0]).Build()).AsInt64()
    if nil != ttlErr {
        fail("%s: ask redis for the accepted nonce out of band: %v", internalAuthLabel, ttlErr)
    }

    /* PTTL answers -2 for a key that does not exist and -1 for one without an expiry */
    horizonMilliseconds := (payload.ExpiresAt-time.Now().Unix()+1)*1000
    if 0 >= remainingMilliseconds || horizonMilliseconds < remainingMilliseconds {
        fail("%s: the accepted nonce under %s answered pttl %d, wanted a key remembered at most as long as the envelope's %d ms — the firewall is not on the shared guard", internalAuthLabel, internalAuthNoncePrefix, remainingMilliseconds, horizonMilliseconds)
    }

    pass("%s: the accepted envelope's nonce is remembered in redis under %s for %d ms, the shared guard every replica reads", internalAuthLabel, internalAuthNoncePrefix, remainingMilliseconds)
}

