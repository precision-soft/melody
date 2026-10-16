package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
)

const translationLabel = "translation"

/* the example's greeting route carries the locale as its first path segment, validated by the router against the route's list (en, ro, ro-RO), and config/security.go makes the prefix public under exactly those locales, so every probe below is anonymous. The handler serves the locale the route matched (handler/i18n/greeting_handler.go); ?name= fills the greeting placeholder and ?count= drives the ICU plural. */
func translationRoute(locale string) string {
    return "/" + locale + "/i18n/greeting/"
}

/* translationPayload mirrors handler/i18n.GreetingResponse. */
type translationPayload struct {
    Locale   string `json:"locale"`
    Greeting string `json:"greeting"`
    Cart     string `json:"cart"`
}

/* runTranslationCheck drives the translation catalogues over live http: both shipped locales, all three ICU plural
branches including the EXACT-ZERO one (=0 is a separate branch from "other" and a matcher that dropped it would
answer the "other" form for zero, which reads as "0 items in your cart" — a plausible regression that no status
assertion notices), and the fallback chain.

Two things this section deliberately does NOT do:

  - it sends no Accept-Language header. v3 negotiates only Accept (html versus json, http/accept.go) and
    Request.Locale() reads the _locale ROUTE attribute; no code path anywhere in v3 reads Accept-Language, so an
    assertion on it would pass no matter what the framework did and would document a feature that does not exist;
  - it does not re-prove the login entry point's 302, which the per-major sections already cover. It proves the
    opposite case instead: the PER-FIREWALL entry point below. */
func runTranslationCheck(baseUrl string) {
    client := newLiveExampleClient(baseUrl)

    assertTranslationCatalogue(client, "en", "Ada", "Hello, Ada!")
    assertTranslationCatalogue(client, "ro", "Ada", "Salut, Ada!")

    assertTranslationPlural(client, "en", 0, "Your cart is empty")
    assertTranslationPlural(client, "en", 1, "1 item in your cart")
    assertTranslationPlural(client, "en", 7, "7 items in your cart")

    assertTranslationPlural(client, "ro", 0, "Coșul este gol")
    assertTranslationPlural(client, "ro", 1, "1 produs în coș")
    assertTranslationPlural(client, "ro", 7, "7 produse în coș")
    assertTranslationPlural(client, "ro", 20, "20 de produse în coș")
    assertTranslationPlural(client, "ro", 101, "101 produse în coș")

    pass("both catalogues answered their ICU plural branches, the exact-zero one included, and Romanian's few apart from its other")

    assertTranslationSuccessNegotiation(client)
    assertTranslationUnservedLocale(client)
    assertTranslationRegionFallback(client)
    assertTokenFirewallJsonEntryPoint(client)
}

func assertTranslationCatalogue(client *liveExampleClient, locale string, name string, expected string) {
    payload := translationPayload{}
    path := fmt.Sprintf("%s?name=%s&count=1", translationRoute(locale), name)

    response := client.get(translationLabel, path)
    requireLiveExampleStatus(translationLabel, path, response, http.StatusOK)
    decodeLiveExamplePayload(translationLabel, response, &payload)

    if expected != payload.Greeting {
        fail(
            "%s: the %s catalogue rendered the greeting as %q, wanted %q",
            translationLabel,
            locale,
            payload.Greeting,
            expected,
        )
    }
    if locale != payload.Locale {
        fail("%s: the handler echoed locale %q for a request that asked for %q", translationLabel, payload.Locale, locale)
    }

    pass("the %s catalogue rendered the greeting with its placeholder filled (%q)", locale, payload.Greeting)
}

func assertTranslationPlural(client *liveExampleClient, locale string, count int, expected string) {
    payload := translationPayload{}
    path := fmt.Sprintf("%s?count=%d", translationRoute(locale), count)

    response := client.get(translationLabel, path)
    requireLiveExampleStatus(translationLabel, path, response, http.StatusOK)
    decodeLiveExamplePayload(translationLabel, response, &payload)

    if expected != payload.Cart {
        fail(
            "%s: the %s catalogue rendered count=%d as %q, wanted %q",
            translationLabel,
            locale,
            count,
            payload.Cart,
            expected,
        )
    }
}

/* assertTranslationUnservedLocale proves the locale is the ROUTE's to validate: a locale the greeting's list does not name matches no route and is not under the public rule, so an anonymous caller is refused by the catch-all instead of being served a fallback, and the old unprefixed spelling, which carried the locale in its query, is refused the same way. The translator's own fallback for an unknown locale is no longer reachable through this door; its unit tests pin it. */
func assertTranslationUnservedLocale(client *liveExampleClient) {
    for _, path := range []string{translationRoute("de") + "?name=Ada", "/i18n/greeting/?locale=ro&name=Ada"} {
        response := client.get(translationLabel, path)
        if http.StatusUnauthorized != response.statusCode || true == strings.Contains(response.bodyText(), "Ada") {
            fail("%s: %s answered %d %s, wanted the catch-all's 401 and no greeting", translationLabel, path, response.statusCode, exampleTruncate(response.bodyText()))
        }
    }

    pass("an unlisted locale and the old query spelling are refused 401 by the catch-all rather than served a greeting")
}

/* assertTranslationRegionFallback is the second link of the chain: a region-qualified locale with no catalogue of its own resolves to its base language. A resolver that only ever matched the whole tag would silently serve every "ro-RO" reader the english text, which is a defect no error surfaces. */
func assertTranslationRegionFallback(client *liveExampleClient) {
    payload := translationPayload{}
    path := translationRoute("ro-RO") + "?name=Ada&count=1"

    response := client.get(translationLabel, path)
    requireLiveExampleStatus(translationLabel, path, response, http.StatusOK)
    decodeLiveExamplePayload(translationLabel, response, &payload)

    if "Salut, Ada!" != payload.Greeting {
        fail(
            "%s: ro-RO rendered the greeting as %q, wanted the base ro catalogue's %q — the region-qualified tag did not fall back to its language",
            translationLabel,
            payload.Greeting,
            "Salut, Ada!",
        )
    }
    if "1 produs în coș" != payload.Cart {
        fail("%s: ro-RO rendered count=1 as %q, wanted the base ro catalogue's singular", translationLabel, payload.Cart)
    }

    pass("a region-qualified locale (ro-RO) fell back to its base language catalogue")
}

/* assertTokenFirewallJsonEntryPoint proves the PER-FIREWALL entry point overrides the global one. The example gives the token firewall on /secure a JsonEntryPoint (config/security.go) while the global entry point redirects to the login page, so a browser-shaped request — Accept: text/html, no credential — must be answered with a 401 json envelope and NOT with a 302 to /login/.

   The regression this catches is a real one and it is silent from the browser's side: an api firewall that inherited the global redirect answers 302 to every unauthenticated api call, so a fetch() follows it and receives the login PAGE with a 200, and the client parses html as its payload instead of seeing the 401 it must handle. */
func assertTokenFirewallJsonEntryPoint(client *liveExampleClient) {
    path := "/secure/me/"

    response := client.call(translationLabel, liveExampleRequest{
        method:     "GET",
        path:       path,
        headerList: map[string]string{"Accept": "text/html"},
    })

    if http.StatusFound == response.statusCode {
        fail(
            "%s: %s answered 302 to %q for an html request — the token firewall inherited the global login entry point instead of its own json one, so an api client follows the redirect and parses the login page as its payload",
            translationLabel,
            path,
            response.location,
        )
    }
    if http.StatusUnauthorized != response.statusCode {
        fail(
            "%s: %s answered %d to an unauthenticated html request, wanted 401 from the token firewall's json entry point: %s",
            translationLabel,
            path,
            response.statusCode,
            exampleTruncate(response.bodyText()),
        )
    }
    if "" != response.location {
        fail("%s: the 401 from %s carried a Location header (%q), so it is still redirecting", translationLabel, path, response.location)
    }

    /* a 401 with an html body would mean the json entry point was bypassed by the content negotiation the Accept header asked for, which is the same defect wearing a different status code */
    if false == strings.Contains(response.contentType, "application/json") {
        fail(
            "%s: the 401 from %s is %q, wanted application/json from the firewall's own entry point",
            translationLabel,
            path,
            response.contentType,
        )
    }

    /* the body is the FRAMEWORK's json error body ({"error":...,"time":...} from http.JsonErrorResponse), not the example presenter's success/payload/errors envelope: the refusal is produced by the firewall's entry point before any handler runs, so nothing in the application shapes it */
    errorPayload := struct {
        Error string `json:"error"`
        Time  string `json:"time"`
    }{}
    if decodeErr := json.Unmarshal(response.body, &errorPayload); nil != decodeErr {
        fail(
            "%s: the 401 from %s is not the framework's json error body (%v): %s",
            translationLabel,
            path,
            decodeErr,
            exampleTruncate(response.bodyText()),
        )
    }
    if "" == errorPayload.Error || "" == errorPayload.Time {
        fail("%s: the 401 from %s carries no error message or no time: %s", translationLabel, path, exampleTruncate(response.bodyText()))
    }

    /* the WWW-Authenticate header is the sharpest available proof of WHICH entry point ran: only the json one sets it, so its absence means the refusal came from somewhere else even when the status happens to be 401 */
    if "" == response.headerList.Get("WWW-Authenticate") {
        fail(
            "%s: the 401 from %s carries no WWW-Authenticate header — the refusal did not come from the token firewall's json entry point",
            translationLabel,
            path,
        )
    }

    pass(
        "the token firewall answered 401 json (error=%q, WWW-Authenticate=%q) to an Accept: text/html request — its entry point overrides the global 302",
        errorPayload.Error,
        response.headerList.Get("WWW-Authenticate"),
    )
}

/* the success path negotiates as the error path does, through the example's presenter, which already answers a door's result the way the framework's WrapResultHandler would: an Accept refusing every type the manager produces is answered 406 with no body, where an error keeps its own status, and a text/plain client reads the success envelope as the plain-text serializer renders a struct, the %v form, pinned here as it IS: a readable success body is the plain-text serializer rendering structured values, which is the next major's to make, not this one's */
func assertTranslationSuccessNegotiation(client *liveExampleClient) {
    path := translationRoute("ro") + "?name=Ada&count=2"

    refused := client.call(translationLabel, liveExampleRequest{method: "GET", path: path, headerList: map[string]string{"Accept": "application/json;q=0, text/plain;q=0"}})
    if http.StatusNotAcceptable != refused.statusCode || 0 != len(refused.body) {
        fail("%s: a greeting asked for with every type refused answered %d %q, wanted 406 with no body", translationLabel, refused.statusCode, refused.body)
    }

    text := client.call(translationLabel, liveExampleRequest{method: "GET", path: path, headerList: map[string]string{"Accept": "text/plain"}})
    if http.StatusOK != text.statusCode || false == strings.HasPrefix(text.contentType, "text/plain") || "{true {ro Salut, Ada! 2 produse în coș} [] map[] []}" != string(text.body) {
        fail("%s: a greeting asked for as text/plain answered %d %q %q, wanted 200 and the serializer's rendering of the envelope", translationLabel, text.statusCode, text.contentType, text.body)
    }

    pass("%s: the greeting answers 406 with no body when every type is refused, and a text/plain client the serializer's %%v rendering of the envelope", translationLabel)
}

