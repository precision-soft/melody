package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
)

const translationLabel = "translation"

/* the example's greeting route is public (config/security.go lists ^/i18n), so every probe below is anonymous.
The handler reads the locale from the ?locale= QUERY parameter (handler/i18n/greeting_handler.go) and hands it to
the translator; ?name= fills the greeting placeholder and ?count= drives the ICU plural. */
const translationRoute = "/i18n/greeting/"

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

    pass("both catalogues answered all three ICU plural branches, including the exact-zero one")

    assertTranslationFallback(client)
    assertTranslationRegionFallback(client)
    assertTokenFirewallJsonEntryPoint(client)
}

func assertTranslationCatalogue(client *liveExampleClient, locale string, name string, expected string) {
    payload := translationPayload{}
    path := fmt.Sprintf("%s?locale=%s&name=%s&count=1", translationRoute, locale, name)

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
    path := fmt.Sprintf("%s?locale=%s&count=%d", translationRoute, locale, count)

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

/* assertTranslationFallback proves the fallback CHAIN rather than the mere absence of an error. An unknown locale
must render the default catalogue's message; the failure it guards against is a translator that fell through to
returning the message ID itself ("greeting", "cart.items"), which is what a missing-catalogue path degrades to and
what a caller then renders straight into a page. */
func assertTranslationFallback(client *liveExampleClient) {
    payload := translationPayload{}
    path := translationRoute + "?locale=de&name=Ada&count=2"

    response := client.get(translationLabel, path)
    requireLiveExampleStatus(translationLabel, path, response, http.StatusOK)
    decodeLiveExamplePayload(translationLabel, response, &payload)

    for _, rendered := range []string{payload.Greeting, payload.Cart} {
        if true == translationLooksLikeMessageIdentifier(rendered) {
            fail(
                "%s: an unknown locale rendered the raw message id %q instead of falling back to the default catalogue",
                translationLabel,
                rendered,
            )
        }
    }

    if "Hello, Ada!" != payload.Greeting {
        fail(
            "%s: an unknown locale rendered the greeting as %q, wanted the default catalogue's %q",
            translationLabel,
            payload.Greeting,
            "Hello, Ada!",
        )
    }
    if "2 items in your cart" != payload.Cart {
        fail(
            "%s: an unknown locale rendered count=2 as %q, wanted the default catalogue's plural form",
            translationLabel,
            payload.Cart,
        )
    }

    pass("an unknown locale fell back to the default catalogue instead of returning the raw message id")
}

/* assertTranslationRegionFallback is the second link of the chain: a region-qualified locale with no catalogue of
its own resolves to its base language. A resolver that only ever matched the whole tag would silently serve every
"ro-RO" reader the english text, which is a defect no error surfaces. */
func assertTranslationRegionFallback(client *liveExampleClient) {
    payload := translationPayload{}
    path := translationRoute + "?locale=ro-RO&name=Ada&count=1"

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

func translationLooksLikeMessageIdentifier(rendered string) bool {
    return "greeting" == rendered || "cart.items" == rendered
}

/* assertTokenFirewallJsonEntryPoint proves the PER-FIREWALL entry point overrides the global one. The example gives
the token firewall on /secure a JsonEntryPoint (config/security.go) while the global entry point redirects to the
login page, so a browser-shaped request — Accept: text/html, no credential — must be answered with a 401 json
envelope and NOT with a 302 to /login/.

The regression this catches is a real one and it is silent from the browser's side: an api firewall that inherited
the global redirect answers 302 to every unauthenticated api call, so a fetch() follows it and receives the login
PAGE with a 200, and the client parses html as its payload instead of seeing the 401 it must handle. */
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

    /* a 401 with an html body would mean the json entry point was bypassed by the content negotiation the Accept
       header asked for, which is the same defect wearing a different status code */
    if false == strings.Contains(response.contentType, "application/json") {
        fail(
            "%s: the 401 from %s is %q, wanted application/json from the firewall's own entry point",
            translationLabel,
            path,
            response.contentType,
        )
    }

    /* the body is the FRAMEWORK's standardized error envelope ({"status":...,"time":...,"error":{"message":...}}
       from http.JsonErrorResponse), not the example presenter's success/payload/errors envelope: the refusal is
       produced by the firewall's entry point before any handler runs, so nothing in the application shapes it */
    errorPayload := struct {
        Status int    `json:"status"`
        Time   string `json:"time"`
        Error  struct {
            Message string `json:"message"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal(response.body, &errorPayload); nil != decodeErr {
        fail(
            "%s: the 401 from %s is not the framework's json error envelope (%v): %s",
            translationLabel,
            path,
            decodeErr,
            exampleTruncate(response.bodyText()),
        )
    }
    if "" == errorPayload.Error.Message {
        fail("%s: the 401 from %s carries no error message: %s", translationLabel, path, exampleTruncate(response.bodyText()))
    }

    /* the WWW-Authenticate header is the sharpest available proof of WHICH entry point ran: only the json one sets
       it, so its absence means the refusal came from somewhere else even when the status happens to be 401 */
    if "" == response.headerList.Get("WWW-Authenticate") {
        fail(
            "%s: the 401 from %s carries no WWW-Authenticate header — the refusal did not come from the token firewall's json entry point",
            translationLabel,
            path,
        )
    }

    pass(
        "the token firewall answered 401 json (error=%q, WWW-Authenticate=%q) to an Accept: text/html request — its entry point overrides the global 302",
        errorPayload.Error.Message,
        response.headerList.Get("WWW-Authenticate"),
    )
}
