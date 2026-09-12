package main

import (
    "compress/gzip"
    "context"
    "encoding/json"
    "io"
    "net/http"
    "strings"
)

/* The v1 example's showcase wirings, driven over the wire. The origin and the api token are the values its .env ships (APP_CORS_ALLOW_ORIGINS, APP_API_TOKEN); the forwarded address is a TEST-NET-3 literal no real client carries, so finding it in a rate-limit key can only mean the forwarded header was believed. */
const (
    exampleShowcaseAllowedOrigin    = "https://catalog.example.test"
    exampleShowcaseDisallowedOrigin = "https://elsewhere.example.test"
    exampleShowcaseApiKeyHeader     = "X-Api-Key"
    exampleShowcaseApiToken         = "example-api-token"
    exampleShowcaseForwardedFor     = "203.0.113.77"
)

/* runExampleShowcaseAssertions covers the wirings only the v1 example carries: the cors listeners, the api-key firewall, gzip compression, per-field validation errors and the trusted-proxy client address. It runs between the login flow and the integration demos, so the throttled writes it spends are cleared by the rate-limit subsection's own reset before that section counts an exact budget. */
func runExampleShowcaseAssertions(major exampleMajor, application *exampleApplication, redisAddress string) {
    assertExampleCorsPreflight(major)
    assertExampleCorsDecoratesARefusal(major)
    assertExampleApiKeyFirewall(major)
    assertExampleGzipCompression(major)
    assertExampleValidationAnswersPerField(major, application)
    assertExampleIdentityAndGrammarDoors(major, application)
    assertExampleForwardedAddressKeysTheLimiter(major, application, redisAddress)
    assertExampleStaticCacheValidators(major)
}

/* assertExampleStaticCacheValidators proves the static cache the v1 example arms in its .env (MELODY_STATIC_ENABLE_CACHE): a tracked asset answers with the validators and the cache policy, a client replaying the ETag is answered 304 with no body, a stale ETag gets the bytes again, and the two validators keep their precedence — If-Modified-Since is consulted only when no If-None-Match was offered, so a mismatched ETag wins over a matching date. The values are read from the live answer rather than assumed, because the workspace copy does not preserve the source tree's modification times. */
func assertExampleStaticCacheValidators(major exampleMajor) {
    client := newExampleClient(major)

    first := client.call("GET", exampleStaticAsset, "", "", "")
    if http.StatusOK != first.statusCode {
        fail("[%s] %s answered %d, wanted 200 with the asset", major.label, exampleStaticAsset, first.statusCode)
    }

    etag := first.headerList.Get("ETag")
    lastModified := first.headerList.Get("Last-Modified")
    cacheControl := first.headerList.Get("Cache-Control")

    if "" == etag {
        fail("[%s] %s carries no ETag while the static cache is armed", major.label, exampleStaticAsset)
    }
    if "" == lastModified {
        fail("[%s] %s carries no Last-Modified while served from the filesystem", major.label, exampleStaticAsset)
    }
    if "public, max-age=3600" != cacheControl {
        fail("[%s] %s carries Cache-Control %q, wanted the armed policy \"public, max-age=3600\"", major.label, exampleStaticAsset, cacheControl)
    }
    pass("[%s] %s answers with ETag, Last-Modified and the cache policy", major.label, exampleStaticAsset)

    replayed := client.callWithHeaderList("GET", exampleStaticAsset, "", "", map[string]string{
        "If-None-Match": etag,
    }, "")
    if http.StatusNotModified != replayed.statusCode {
        fail("[%s] replaying the ETag answered %d, wanted 304", major.label, replayed.statusCode)
    }
    if "" != replayed.body {
        fail("[%s] the 304 carried %d bytes of body", major.label, len(replayed.body))
    }
    pass("[%s] a replayed ETag is answered 304 with no body", major.label)

    stale := client.callWithHeaderList("GET", exampleStaticAsset, "", "", map[string]string{
        "If-None-Match": `"not-the-etag"`,
    }, "")
    if http.StatusOK != stale.statusCode {
        fail("[%s] a stale ETag answered %d, wanted the asset again", major.label, stale.statusCode)
    }
    pass("[%s] a stale ETag is answered the asset again", major.label)

    dated := client.callWithHeaderList("GET", exampleStaticAsset, "", "", map[string]string{
        "If-Modified-Since": lastModified,
    }, "")
    if http.StatusNotModified != dated.statusCode {
        fail("[%s] replaying Last-Modified answered %d, wanted 304", major.label, dated.statusCode)
    }
    pass("[%s] a replayed Last-Modified is answered 304", major.label)

    /* the negative that separates the precedence from a date-only implementation: the date says unchanged, the ETag says stale, and an implementation consulting the date despite the offered ETag answers 304 here */
    precedence := client.callWithHeaderList("GET", exampleStaticAsset, "", "", map[string]string{
        "If-None-Match":     `"not-the-etag"`,
        "If-Modified-Since": lastModified,
    }, "")
    if http.StatusOK != precedence.statusCode {
        fail("[%s] a stale ETag beside a matching date answered %d, wanted 200 — If-Modified-Since was consulted despite the offered ETag", major.label, precedence.statusCode)
    }
    pass("[%s] the offered ETag silences If-Modified-Since, as the precedence demands", major.label)
}

/* assertExampleCorsPreflight asks the browser's question before a cross-origin POST: OPTIONS with the origin and the requested method and header. The route is registered for POST alone, so the 204 can only come from the cors request listener answering AHEAD of routing and of the security chain — an anonymous client, no cookie, and a path whose access control demands ROLE_EDITOR. */
func assertExampleCorsPreflight(major exampleMajor) {
    client := newExampleClient(major)

    preflight := client.callWithHeaderList("OPTIONS", exampleProductCreateRoute, "", "", map[string]string{
        "Origin":                         exampleShowcaseAllowedOrigin,
        "Access-Control-Request-Method":  "POST",
        "Access-Control-Request-Headers": exampleShowcaseApiKeyHeader,
    }, "")

    if http.StatusNoContent != preflight.statusCode {
        fail("[%s] the preflight for %s answered %d, wanted 204 from the cors request listener", major.label, exampleProductCreateRoute, preflight.statusCode)
    }
    if exampleShowcaseAllowedOrigin != preflight.headerList.Get("Access-Control-Allow-Origin") {
        fail("[%s] the preflight echoed %q as the allowed origin, wanted %q", major.label, preflight.headerList.Get("Access-Control-Allow-Origin"), exampleShowcaseAllowedOrigin)
    }
    if false == strings.Contains(preflight.headerList.Get("Access-Control-Allow-Methods"), "POST") {
        fail("[%s] the preflight allows %q, which does not carry POST", major.label, preflight.headerList.Get("Access-Control-Allow-Methods"))
    }
    if false == strings.Contains(strings.ToLower(preflight.headerList.Get("Access-Control-Allow-Headers")), strings.ToLower(exampleShowcaseApiKeyHeader)) {
        fail("[%s] the preflight allows headers %q, which does not carry %s", major.label, preflight.headerList.Get("Access-Control-Allow-Headers"), exampleShowcaseApiKeyHeader)
    }
    pass("[%s] the cors preflight for %s is answered 204 before routing and security", major.label, exampleProductCreateRoute)

    refused := client.callWithHeaderList("OPTIONS", exampleProductCreateRoute, "", "", map[string]string{
        "Origin":                        exampleShowcaseDisallowedOrigin,
        "Access-Control-Request-Method": "POST",
    }, "")

    if "" != refused.headerList.Get("Access-Control-Allow-Origin") {
        fail("[%s] a preflight from a disallowed origin was granted %q", major.label, refused.headerList.Get("Access-Control-Allow-Origin"))
    }
    pass("[%s] a preflight from a disallowed origin is granted nothing", major.label)
}

/* assertExampleCorsDecoratesARefusal is the reason the example registers the cors LISTENERS rather than the middleware: a security refusal never enters the middleware chain, and without the response listener a browser would see a cors failure where the server said 401 — the refusal a client is supposed to branch on. */
func assertExampleCorsDecoratesARefusal(major exampleMajor) {
    client := newExampleClient(major)

    refused := client.callWithHeaderList("GET", exampleProductListRoute, "", "", map[string]string{
        "Origin": exampleShowcaseAllowedOrigin,
    }, "")

    if http.StatusUnauthorized != refused.statusCode {
        fail("[%s] %s answered %d to the anonymous cross-origin request, wanted 401", major.label, exampleProductListRoute, refused.statusCode)
    }
    if exampleShowcaseAllowedOrigin != refused.headerList.Get("Access-Control-Allow-Origin") {
        fail("[%s] the 401 carries no allowed origin, so a browser reads a cors failure instead of the refusal", major.label)
    }
    pass("[%s] the security refusal itself carries the cors headers (listeners, not middleware)", major.label)
}

/* assertExampleApiKeyFirewall proves the door APP_API_TOKEN promises: the key alone reaches the catalogue, no session involved, and a wrong key is a 401 — not a fall-through to some weaker rule. The matcher claims only requests that PRESENT the header, which the cookie-borne flows all around this probe keep proving from the other side: they carry no key and keep working. */
func assertExampleApiKeyFirewall(major exampleMajor) {
    client := newExampleClient(major)

    granted := client.callWithHeaderList("GET", exampleProductListRoute, "", "", map[string]string{
        exampleShowcaseApiKeyHeader: exampleShowcaseApiToken,
    }, "")

    if http.StatusOK != granted.statusCode {
        fail("[%s] %s answered %d to the api key, wanted 200: %s", major.label, exampleProductListRoute, granted.statusCode, exampleTruncate(granted.body))
    }
    if false == strings.Contains(granted.body, "prod-1") {
        fail("[%s] the api-key answer carries no seeded product: %s", major.label, exampleTruncate(granted.body))
    }
    if nil != client.sessionCookie() {
        fail("[%s] the api-key request was answered with a session cookie, so the stateless firewall was not the one answering", major.label)
    }
    pass("[%s] the api key alone reads the catalogue through the stateless firewall", major.label)

    refused := client.callWithHeaderList("GET", exampleProductListRoute, "", "", map[string]string{
        exampleShowcaseApiKeyHeader: "not-the-token",
    }, "")

    if http.StatusUnauthorized != refused.statusCode {
        fail("[%s] a wrong api key answered %d, wanted 401", major.label, refused.statusCode)
    }
    pass("[%s] a wrong api key is refused with 401", major.label)
}

/* assertExampleGzipCompression reads the listing twice through the api-key door: once as identity, once asking for gzip. The identity read also guards the probe's own premise — the compression middleware only compresses bodies of at least a kilobyte, so a seed catalogue that shrank below that would quietly turn the second half into a test of nothing. */
func assertExampleGzipCompression(major exampleMajor) {
    client := newExampleClient(major)

    identity := client.callWithHeaderList("GET", exampleProductListRoute, "", "", map[string]string{
        exampleShowcaseApiKeyHeader: exampleShowcaseApiToken,
        "Accept-Encoding":           "identity",
    }, "")

    if http.StatusOK != identity.statusCode {
        fail("[%s] %s answered %d to the identity read, wanted 200", major.label, exampleProductListRoute, identity.statusCode)
    }
    if 1024 > len(identity.body) {
        fail(
            "[%s] the listing is %d bytes, under the 1024-byte compression floor — the seed catalogue (repository/seed.go) shrank and the gzip probe would prove nothing",
            major.label,
            len(identity.body),
        )
    }

    compressed := client.callWithHeaderList("GET", exampleProductListRoute, "", "", map[string]string{
        exampleShowcaseApiKeyHeader: exampleShowcaseApiToken,
        "Accept-Encoding":           "gzip",
    }, "")

    if http.StatusOK != compressed.statusCode {
        fail("[%s] %s answered %d to the gzip read, wanted 200", major.label, exampleProductListRoute, compressed.statusCode)
    }
    if "gzip" != compressed.headerList.Get("Content-Encoding") {
        fail("[%s] the gzip read answered Content-Encoding %q, wanted gzip", major.label, compressed.headerList.Get("Content-Encoding"))
    }
    /* the cors response listener adds its own Vary: Origin line, so the field is read whole rather than as its first line */
    if false == strings.Contains(strings.Join(compressed.headerList.Values("Vary"), ", "), "Accept-Encoding") {
        fail("[%s] the compressed answer does not vary on Accept-Encoding, so a shared cache may hand gzip to an identity client", major.label)
    }

    gzipReader, gzipErr := gzip.NewReader(strings.NewReader(compressed.body))
    if nil != gzipErr {
        fail("[%s] the compressed body does not open as gzip: %v", major.label, gzipErr)
    }
    inflated, inflateErr := io.ReadAll(gzipReader)
    if nil != inflateErr {
        fail("[%s] the compressed body does not inflate: %v", major.label, inflateErr)
    }
    if string(inflated) != identity.body {
        fail("[%s] the inflated body differs from the identity body, so the compression is not transparent", major.label)
    }
    pass("[%s] the listing travels gzip-compressed (%d bytes over %d) and inflates to the identity answer", major.label, len(compressed.body), len(identity.body))
}

/* assertExampleValidationAnswersPerField sends the two-mistake payload: a one-letter name under the minimum and a negative price. The point is the SHAPE of the refusal — one errors entry per violated field, not one joined string a client would have to split back apart. */
func assertExampleValidationAnswersPerField(major exampleMajor, application *exampleApplication) {
    editor := exampleEditorClient(major, application)

    invalid := editor.call(
        "POST",
        exampleProductCreateRoute,
        "application/json",
        "application/json",
        `{"id":"","name":"x","description":"probe","categoryId":"cat-1","price":-1,"currencyId":"cur-eur","stock":1}`,
    )

    if http.StatusBadRequest != invalid.statusCode {
        fail("[%s] the two-mistake payload answered %d, wanted 400: %s", major.label, invalid.statusCode, exampleTruncate(invalid.body))
    }

    var envelope struct {
        Success bool     `json:"success"`
        Errors  []string `json:"errors"`
    }
    if decodeErr := json.Unmarshal([]byte(invalid.body), &envelope); nil != decodeErr {
        fail("[%s] the validation refusal is not the api envelope (%v): %s", major.label, decodeErr, exampleTruncate(invalid.body))
    }

    if true == envelope.Success {
        fail("[%s] the validation refusal reports success", major.label)
    }
    if 2 != len(envelope.Errors) {
        fail("[%s] the two violations produced %d errors entries, wanted one per field: %v", major.label, len(envelope.Errors), envelope.Errors)
    }

    var nameEntry, priceEntry string
    for _, entry := range envelope.Errors {
        if strings.HasPrefix(entry, "name:") {
            nameEntry = entry
        }
        if strings.HasPrefix(entry, "price:") {
            priceEntry = entry
        }
    }
    if "" == nameEntry || "" == priceEntry {
        fail("[%s] the errors entries do not name the violated fields: %v", major.label, envelope.Errors)
    }
    pass("[%s] a two-mistake payload is refused 400 with one errors entry per field", major.label)
}

/* assertExampleForwardedAddressKeysTheLimiter proves the trusted-proxy wiring end to end, out of band: a write carrying X-Forwarded-For from the harness (a loopback peer, which the example's trusted list includes) must be budgeted against the FORWARDED address, so a rate-limit key naming the TEST-NET-3 literal must appear in redis. Without the resolver the key would name the peer and the forwarded header would be decoration. */
func assertExampleForwardedAddressKeysTheLimiter(major exampleMajor, application *exampleApplication, redisAddress string) {
    if "" == redisAddress {
        skip("[%s] REDIS_ADDRESS is not set, so the forwarded-address probe has no out-of-band half", major.label)

        return
    }

    prefix := exampleMajorRateLimitPrefix(major)
    resetExampleRateLimitCounters("["+major.label+"] forwarded-address probe", redisAddress, prefix)

    editor := exampleEditorClient(major, application)

    forwarded := editor.callWithHeaderList(
        "POST",
        exampleProductCreateRoute,
        "application/json",
        "application/json",
        map[string]string{"X-Forwarded-For": exampleShowcaseForwardedFor},
        `{"id":"","name":"x","description":"probe","categoryId":"cat-1","price":-1,"currencyId":"cur-eur","stock":1}`,
    )

    /* the payload is the same two-mistake refusal as above on purpose: the limiter counts the request before the validator refuses it, so the probe spends budget without creating a row to clean up */
    if http.StatusBadRequest != forwarded.statusCode {
        fail("[%s] the forwarded probe answered %d, wanted the validator's 400", major.label, forwarded.statusCode)
    }

    client := openRedis(redisAddress)
    defer client.Close()

    keys, keysErr := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
    if nil != keysErr {
        fail("[%s] list the rate limit keys: %v", major.label, keysErr)
    }

    found := false
    for _, key := range keys {
        if true == strings.Contains(key, exampleShowcaseForwardedFor) {
            found = true
        }
    }
    if false == found {
        fail(
            "[%s] no rate-limit key names %s — the limiter keyed the write by the peer, so the forwarded header was not believed: %v\n%s",
            major.label,
            exampleShowcaseForwardedFor,
            keys,
            application.logTail(10),
        )
    }
    pass("[%s] the throttled write was budgeted against the forwarded address %s (out-of-band redis key)", major.label, exampleShowcaseForwardedFor)

    resetExampleRateLimitCounters("["+major.label+"] forwarded-address probe", redisAddress, prefix)
}

/* assertExampleIdentityAndGrammarDoors proves, over the wire, the two identity repairs only the database can fake and only the cache can hide. The accent probe: the user table's collation folds accents ('café' = 'cafe' under utf8mb4_0900_ai_ci), so before the binary-collation comparison a login under the accent-stripped spelling authenticated a user nobody created under it; the rename probe: the by-username cache entry carries no ttl, so before the previous-username invalidation the OLD spelling kept signing in from the cache after a rename. Both proofs are the NEGATIVE — the refused login — beside the positive control that the proper spelling works; the grammar probe: a product id carrying an interior space used to land in the database and then poison every later cache write, so the door must answer 400 and the listing must never carry it. */
func assertExampleIdentityAndGrammarDoors(major exampleMajor, application *exampleApplication) {
    editor := exampleEditorClient(major, application)

    spaced := editor.call(
        "POST",
        "/products/api/create/",
        "application/json",
        "application/json",
        `{"id":"probe id","name":"Probe","description":"probe","categoryId":"cat-1","price":1,"currencyId":"cur-eur","stock":1}`,
    )
    if http.StatusBadRequest != spaced.statusCode {
        fail("[%s] a product id with an interior space answered %d, wanted 400: %s", major.label, spaced.statusCode, exampleTruncate(spaced.body))
    }

    listing := editor.call("GET", "/products/api/read/", "application/json", "", "")
    if http.StatusOK != listing.statusCode {
        fail("[%s] the product listing answered %d after the refused create", major.label, listing.statusCode)
    }
    if true == strings.Contains(listing.body, "probe id") {
        fail("[%s] the refused product id reached the catalogue anyway", major.label)
    }
    pass("[%s] a product id the cache-key grammar refuses is turned away 400 before the row lands", major.label)

    admin := newExampleClient(major)
    adminSignIn := admin.call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody("admin", "admin"))
    if http.StatusOK != adminSignIn.statusCode {
        fail("[%s] the seeded admin could not sign in (%d): %s", major.label, adminSignIn.statusCode, exampleTruncate(adminSignIn.body))
    }

    /* the user table persists between runs, so a probe user a failed run left behind would turn the create below into "username already exists"; both spellings the probe ever writes are removed first */
    removeExampleProbeUsers(major, admin, "café-probe", "cafe-probe-renamed")

    created := admin.call(
        "POST",
        "/users/api/create/",
        "application/json",
        "application/json",
        `{"username":"café-probe","password":"probe-pass","roles":["ROLE_USER"]}`,
    )
    if http.StatusCreated != created.statusCode {
        fail("[%s] creating the accented probe user answered %d: %s", major.label, created.statusCode, exampleTruncate(created.body))
    }

    createdUser := struct {
        Id string `json:"id"`
    }{}
    decodeExampleData(major.label, "/users/api/create/", created.body, &createdUser)

    stripped := newExampleClient(major).call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody("cafe-probe", "probe-pass"))
    if http.StatusUnauthorized != stripped.statusCode {
        fail("[%s] the accent-stripped spelling signed in (%d) — the lookup is riding the column collation again", major.label, stripped.statusCode)
    }

    properClient := newExampleClient(major)
    proper := properClient.call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody("café-probe", "probe-pass"))
    if http.StatusOK != proper.statusCode {
        fail("[%s] the proper accented spelling could not sign in (%d): %s", major.label, proper.statusCode, exampleTruncate(proper.body))
    }
    pass("[%s] the accent-stripped spelling authenticates nobody while the proper one signs in", major.label)

    renamed := admin.call(
        "PUT",
        "/users/api/update/"+createdUser.Id+"/",
        "application/json",
        "application/json",
        `{"username":"cafe-probe-renamed","password":"","roles":["ROLE_USER"]}`,
    )
    if http.StatusOK != renamed.statusCode {
        fail("[%s] renaming the probe user answered %d: %s", major.label, renamed.statusCode, exampleTruncate(renamed.body))
    }

    oldSpelling := newExampleClient(major).call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody("café-probe", "probe-pass"))
    if http.StatusUnauthorized != oldSpelling.statusCode {
        fail("[%s] the pre-rename spelling still signs in (%d) — the ttl-less cache entry survived the rename", major.label, oldSpelling.statusCode)
    }

    newSpelling := newExampleClient(major).call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody("cafe-probe-renamed", "probe-pass"))
    if http.StatusOK != newSpelling.statusCode {
        fail("[%s] the renamed spelling could not sign in (%d): %s", major.label, newSpelling.statusCode, exampleTruncate(newSpelling.body))
    }
    pass("[%s] a rename closes the old spelling's door and opens the new one", major.label)

    deleted := admin.call("DELETE", "/users/api/delete/"+createdUser.Id+"/", "application/json", "", "")
    if http.StatusOK != deleted.statusCode {
        fail("[%s] removing the probe user answered %d: %s", major.label, deleted.statusCode, exampleTruncate(deleted.body))
    }
}

/* removeExampleProbeUsers deletes every listed username through the admin api, tolerating absence: it exists for the probe rows a failed earlier run may have left in the persistent user table. */
func removeExampleProbeUsers(major exampleMajor, admin *exampleClient, usernames ...string) {
    listing := admin.call("GET", "/users/api/read/", "application/json", "", "")
    if http.StatusOK != listing.statusCode {
        fail("[%s] the user listing answered %d during probe cleanup: %s", major.label, listing.statusCode, exampleTruncate(listing.body))
    }

    users := []struct {
        Id       string `json:"id"`
        Username string `json:"username"`
    }{}
    decodeExampleData(major.label, "/users/api/read/", listing.body, &users)

    for _, user := range users {
        for _, username := range usernames {
            if username != user.Username {
                continue
            }

            deleted := admin.call("DELETE", "/users/api/delete/"+user.Id+"/", "application/json", "", "")
            if http.StatusOK != deleted.statusCode {
                fail("[%s] removing the leftover probe user %q answered %d: %s", major.label, user.Username, deleted.statusCode, exampleTruncate(deleted.body))
            }
        }
    }
}
