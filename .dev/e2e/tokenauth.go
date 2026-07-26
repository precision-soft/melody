package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "net/http"
    "strings"
    "time"
)

const tokenAuthLabel = "token auth over http"

/* the token firewall guards /secure (config/security.go); /secure/me/ echoes the authenticated principal
(handler/secure/me_handler.go), which is what lets the section assert the IDENTITY the token carried and not merely
that a 200 came back. */
const tokenAuthRoute = "/secure/me/"

/* tokenAuthPayload mirrors handler/secure.mePayload. */
type tokenAuthPayload struct {
    UserIdentifier string   `json:"userIdentifier"`
    Roles          []string `json:"roles"`
    Impersonator   *struct {
        UserIdentifier string   `json:"userIdentifier"`
        Roles          []string `json:"roles"`
    } `json:"impersonator,omitempty"`
}

/* tokenAuthForeignSecret is the harness's OWN signing key and it is deliberately not the example's. The wrong-key
negative has to prove that a structurally perfect, unexpired, correctly-claimed token signed by someone else is
refused — and it must do so without the harness ever holding the application's secret, because a harness that knew
the demo secret could not tell "the firewall verifies the signature" apart from "the firewall trusts any token the
harness produced". */
const tokenAuthForeignSecret = "melody-e2e-harness-key-not-the-application-secret"

/* runTokenAuthCheck mints a real credential with the example's own auth:token command and drives the token
firewall with it, then attacks it four ways. Minting through the application is the point: the harness never
carries a copy of the demo jwt secret, so the positive proves the firewall accepts what the application itself
issues rather than what the harness thinks the application should accept. */
func runTokenAuthCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    output, elapsed := runExampleMintCommand(tokenAuthLabel, "auth:token", "--user", "e2e-token-user", "--role", "ROLE_USER")
    token := exampleMintedToken(tokenAuthLabel, output)

    assertTokenAuthAccepted(client, token, elapsed)
    assertTokenAuthRejected(client, "no Authorization header at all", "")
    assertTokenAuthRejected(client, "a token signed with the harness's own secret", tokenAuthForeignToken("e2e-token-user", []string{"ROLE_USER"}))
    assertTokenAuthRejected(client, "a token whose signature was flipped", tokenAuthTamperedSignature(token))
    assertTokenAuthRejected(client, "an alg:none token", tokenAuthAlgorithmNoneToken("e2e-token-user", []string{"ROLE_USER"}))
}

func assertTokenAuthAccepted(client *liveExampleClient, token string, mintElapsed time.Duration) {
    response := client.call(tokenAuthLabel, liveExampleRequest{
        method:     "GET",
        path:       tokenAuthRoute,
        headerList: map[string]string{"Authorization": "Bearer " + token},
    })

    if http.StatusUnauthorized == response.statusCode {
        fail(
            "%s: the token the application's own auth:token command minted was refused with 401%s",
            tokenAuthLabel,
            exampleMintDelayDiagnostic(mintElapsed),
        )
    }
    requireLiveExampleStatus(tokenAuthLabel, tokenAuthRoute+" with a minted bearer token", response, http.StatusOK)

    payload := tokenAuthPayload{}
    decodeLiveExamplePayload(tokenAuthLabel, response, &payload)

    /* the identity is asserted, not just the status: a firewall that authenticated the request as somebody else —
       an anonymous principal that happened to satisfy the access rule, or a cached identity from another token —
       would answer 200 all the same */
    if "e2e-token-user" != payload.UserIdentifier {
        fail(
            "%s: the route reports userIdentifier %q, wanted the token's subject %q",
            tokenAuthLabel,
            payload.UserIdentifier,
            "e2e-token-user",
        )
    }
    if false == tokenAuthHasRole(payload.Roles, "ROLE_USER") {
        fail("%s: the authenticated principal holds roles %v, wanted the token's ROLE_USER", tokenAuthLabel, payload.Roles)
    }
    if nil != payload.Impersonator {
        fail("%s: a plain token produced an impersonator (%q) — nothing asked for a switch", tokenAuthLabel, payload.Impersonator.UserIdentifier)
    }

    pass("the minted bearer token authenticated as its own subject %q with roles %v", payload.UserIdentifier, payload.Roles)
}

func assertTokenAuthRejected(client *liveExampleClient, what string, token string) {
    headerList := map[string]string{}
    if "" != token {
        headerList["Authorization"] = "Bearer " + token
    }

    response := client.call(tokenAuthLabel, liveExampleRequest{
        method:     "GET",
        path:       tokenAuthRoute,
        headerList: headerList,
    })

    if http.StatusUnauthorized != response.statusCode {
        fail(
            "%s: %s was answered %d, wanted 401 — the firewall admitted a credential it must refuse: %s",
            tokenAuthLabel,
            what,
            response.statusCode,
            exampleTruncate(response.bodyText()),
        )
    }

    pass("%s was refused with 401", what)
}

func tokenAuthHasRole(roles []string, wanted string) bool {
    for _, role := range roles {
        if wanted == role {
            return true
        }
    }

    return false
}

/* tokenAuthForeignToken mints a well-formed, unexpired HS256 token signed with the HARNESS's secret. Every claim
the validator reads is correct; only the signing key is wrong, so a firewall that verified the structure but not
the signature would admit it. */
func tokenAuthForeignToken(subject string, roles []string) string {
    now := time.Now()

    return tokenAuthSign(
        map[string]any{"alg": "HS256", "typ": "JWT"},
        map[string]any{
            "sub":   subject,
            "roles": roles,
            "iat":   now.Unix(),
            "exp":   now.Add(time.Hour).Unix(),
        },
        []byte(tokenAuthForeignSecret),
    )
}

/* tokenAuthAlgorithmNoneToken mints the classic unsigned-token attack: the header declares alg "none" and the
signature segment is empty. A validator that dispatched on the declared algorithm instead of pinning its own would
accept it and authenticate whatever the payload claims. */
func tokenAuthAlgorithmNoneToken(subject string, roles []string) string {
    now := time.Now()

    header, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
    payload, _ := json.Marshal(map[string]any{
        "sub":   subject,
        "roles": roles,
        "iat":   now.Unix(),
        "exp":   now.Add(time.Hour).Unix(),
    })

    return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func tokenAuthSign(header map[string]any, claims map[string]any, secret []byte) string {
    headerJson, _ := json.Marshal(header)
    claimsJson, _ := json.Marshal(claims)

    signingInput := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(claimsJson)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

/* tokenAuthTamperedSignature rewrites one character of the signature segment and leaves everything else alone. The
replacement character is chosen so it always differs from the one it replaces: a "tamper" that reproduced the
original token would make the negative vacuous — the firewall would refuse nothing and the check would still be
green. It returns the input unchanged only when there is nothing to flip, and the caller's own assertion then fails
loudly rather than silently passing. */
func tokenAuthTamperedSignature(token string) string {
    lastDot := strings.LastIndex(token, ".")
    if -1 == lastDot || lastDot == len(token)-1 {
        return token
    }

    signature := []byte(token[lastDot+1:])

    replacement := byte('A')
    if replacement == signature[0] {
        replacement = 'B'
    }
    signature[0] = replacement

    return token[:lastDot+1] + string(signature)
}
