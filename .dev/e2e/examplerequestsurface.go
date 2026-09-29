package main

import (
    "encoding/json"
    "net/http"
    "strings"
)

/* exampleRequestSurfaceClaimedRequestId is the identifier the probe claims on its own request. The kernel mints the one a response is filed under, so finding this value in an envelope can only mean the example echoed what the client sent. */
const exampleRequestSurfaceClaimedRequestId = "e2e-claimed-request-id"

/* runExampleRequestSurfaceAssertions drives the request surface the example serves beyond the shared sections, anonymously: a path spelled other than canonically is refused before the firewall reads it, the method doors answer HEAD, OPTIONS and a 405 with the methods the route allows, the error envelope names the request the kernel filed, the static surface serves no dotfile and a malformed session cookie is an anonymous caller rather than a failure. */
func runExampleRequestSurfaceAssertions(major exampleMajor) {
    assertExampleNonCanonicalPathsAreRefusedBeforeTheFirewall(major)
    assertExampleMethodDoors(major)
    assertExampleErrorEnvelopeCarriesTheMintedRequestId(major)
    assertExampleStaticSurfaceServesNoDotfile(major)
    assertExampleMalformedSessionCookieIsAnonymous(major)
    assertExampleRefusingAcceptKeepsTheErrorStatus(major)
}

/* the control is the same route spelled canonically: an anonymous caller is answered 401 there, so a 400 on the doubled slash or the climbing segment is the canonical-path refusal answering first, not the firewall */
func assertExampleNonCanonicalPathsAreRefusedBeforeTheFirewall(major exampleMajor) {
    client := newExampleClient(major)

    canonical := client.call("GET", exampleUserRoute, "application/json", "", "")
    if http.StatusUnauthorized != canonical.statusCode {
        fail("[%s] %s answered %d to an anonymous caller, wanted the firewall's 401 as the control", major.label, exampleUserRoute, canonical.statusCode)
    }

    for _, path := range []string{"/" + exampleUserRoute, "/x/.." + exampleHealthRoute} {
        refused := client.call("GET", path, "application/json", "", "")
        if http.StatusBadRequest != refused.statusCode {
            fail("[%s] %s answered %d, wanted 400: a path whose cleaned form differs must be refused before routing and before the firewall", major.label, path, refused.statusCode)
        }
    }
    pass("[%s] //… and /x/../… are refused 400 to an anonymous caller, where the canonical spelling reaches the firewall's 401", major.label)
}

func assertExampleMethodDoors(major exampleMajor) {
    client := newExampleClient(major)

    head := client.call("HEAD", exampleHealthRoute, "", "", "")
    if http.StatusOK != head.statusCode || "" != head.body {
        fail("[%s] HEAD %s answered %d with %d body bytes, wanted 200 with none", major.label, exampleHealthRoute, head.statusCode, len(head.body))
    }
    pass("[%s] HEAD %s answers 200 with no body", major.label, exampleHealthRoute)

    refused := client.call("DELETE", exampleHealthRoute, "application/json", "", "")
    if http.StatusMethodNotAllowed != refused.statusCode {
        fail("[%s] DELETE %s answered %d, wanted 405", major.label, exampleHealthRoute, refused.statusCode)
    }
    if false == exampleAllowNames(refused.headerList.Get("Allow"), "GET") {
        fail("[%s] the 405 on %s carries Allow %q, which does not name GET", major.label, exampleHealthRoute, refused.headerList.Get("Allow"))
    }
    pass("[%s] DELETE on the GET route %s answers 405 with Allow %q", major.label, exampleHealthRoute, refused.headerList.Get("Allow"))

    /* no Origin and no Access-Control-Request-Method: the cors listener has nothing to answer, so the Allow comes from the router's own OPTIONS door */
    options := client.call("OPTIONS", exampleHealthRoute, "", "", "")
    if http.StatusNoContent != options.statusCode && http.StatusOK != options.statusCode {
        fail("[%s] OPTIONS %s answered %d, wanted the router's own 204", major.label, exampleHealthRoute, options.statusCode)
    }
    if false == exampleAllowNames(options.headerList.Get("Allow"), "GET", "HEAD", "OPTIONS") {
        fail("[%s] OPTIONS %s carries Allow %q, wanted GET, HEAD and OPTIONS", major.label, exampleHealthRoute, options.headerList.Get("Allow"))
    }
    if "" != options.headerList.Get("Access-Control-Allow-Origin") {
        fail("[%s] OPTIONS %s without an Origin was answered as a cors preflight", major.label, exampleHealthRoute)
    }
    pass("[%s] OPTIONS %s outside a preflight answers the computed Allow %q", major.label, exampleHealthRoute, options.headerList.Get("Allow"))
}

/* the envelope and the response header must name one request, and it must be the one the kernel minted: a request id the client claimed on its own request is neither */
func assertExampleErrorEnvelopeCarriesTheMintedRequestId(major exampleMajor) {
    client := newExampleClient(major)

    for _, claimed := range []string{"", exampleRequestSurfaceClaimedRequestId} {
        headerList := map[string]string{}
        if "" != claimed {
            headerList["X-Request-Id"] = claimed
        }

        missing := client.callWithHeaderList("GET", exampleMissingRoute, "application/json", "", headerList, "")
        if http.StatusNotFound != missing.statusCode {
            fail("[%s] %s answered %d, wanted 404", major.label, exampleMissingRoute, missing.statusCode)
        }

        var envelope struct {
            Context struct {
                RequestId string `json:"requestId"`
            } `json:"context"`
        }
        if decodeErr := json.Unmarshal([]byte(missing.body), &envelope); nil != decodeErr {
            fail("[%s] the 404 is not the api envelope (%v): %s", major.label, decodeErr, exampleTruncate(missing.body))
        }

        minted := missing.headerList.Get("X-Request-Id")
        if "" == minted || minted == claimed {
            fail("[%s] the 404 carries X-Request-Id %q, wanted one the kernel minted (claimed %q)", major.label, minted, claimed)
        }
        if minted != envelope.Context.RequestId {
            fail(
                "[%s] the 404 envelope names request %q while the response header names %q (the request claimed %q): the envelope does not carry the id the kernel filed the request under",
                major.label,
                envelope.Context.RequestId,
                minted,
                claimed,
            )
        }
    }
    pass("[%s] the 404 envelope names the request id the response header carries, the kernel's, with and without a client claim", major.label)
}

/* the dotfiles sit under the ROLE_USER catch-all, so an anonymous caller is refused before the static surface is reached; what is asserted is the absence of the file, not the status that spells it. The doubled slash in front of an asset that is served is the canonical-path refusal again, read on the static surface. */
func assertExampleStaticSurfaceServesNoDotfile(major exampleMajor) {
    client := newExampleClient(major)

    for _, path := range []string{"/.env", "/.git/config"} {
        response := client.call("GET", path, "", "", "")
        if http.StatusOK == response.statusCode || strings.Contains(response.body, "MELODY_") || strings.Contains(response.body, "[core]") {
            fail("[%s] %s answered %d with %s: a dotfile must never be served", major.label, path, response.statusCode, exampleTruncate(response.body))
        }
    }

    served := client.call("GET", exampleStaticAsset, "", "", "")
    if http.StatusOK != served.statusCode {
        fail("[%s] %s answered %d, wanted 200 as the control", major.label, exampleStaticAsset, served.statusCode)
    }

    doubled := client.call("GET", "/"+exampleStaticAsset, "", "", "")
    if http.StatusBadRequest != doubled.statusCode {
        fail("[%s] /%s answered %d, wanted 400 where %s is served", major.label, exampleStaticAsset, doubled.statusCode, exampleStaticAsset)
    }
    pass("[%s] /.env and /.git/config are not served, and //%s is refused while %s is served", major.label, strings.TrimPrefix(exampleStaticAsset, "/"), exampleStaticAsset)
}

func assertExampleMalformedSessionCookieIsAnonymous(major exampleMajor) {
    client := newExampleClient(major)

    response := client.callWithHeaderList("GET", exampleUserRoute, "application/json", "", map[string]string{
        "Cookie": exampleSessionCookie + "=nu-e-hex",
    }, "")
    if http.StatusUnauthorized != response.statusCode {
        fail("[%s] %s with a malformed %s cookie answered %d, wanted the anonymous caller's 401", major.label, exampleUserRoute, exampleSessionCookie, response.statusCode)
    }
    pass("[%s] a malformed %s cookie is an anonymous caller (401), not a failure", major.label, exampleSessionCookie)
}

/* exampleAllowNames reports whether an Allow header lists every one of the wanted methods. */
func exampleAllowNames(allow string, wantedList ...string) bool {
    methodList := map[string]bool{}
    for _, method := range strings.Split(allow, ",") {
        methodList[strings.TrimSpace(method)] = true
    }

    for _, wanted := range wantedList {
        if false == methodList[wanted] {
            return false
        }
    }

    return true
}

/* an Accept that refuses every media type does not turn an error the framework renders into a 406: the error's own status is the signal, so it is kept and served the default json body. The door is the canonical-path refusal, which the kernel renders itself — the example's firewall and presenter answer their own errors without negotiating. The control is the same request accepting json; the refusing arm spells the refusal both ways, every registered type at q=0 and the wildcard at q=0 */
func assertExampleRefusingAcceptKeepsTheErrorStatus(major exampleMajor) {
    client := newExampleClient(major)
    path := "/" + exampleUserRoute

    for _, accept := range []string{"application/json", "application/json;q=0, text/plain;q=0", "*/*;q=0"} {
        refused := client.call("GET", path, accept, "", "")
        if http.StatusBadRequest != refused.statusCode {
            fail("[%s] %s with Accept %q answered %d, wanted the refusal's 400 kept rather than a 406", major.label, path, accept, refused.statusCode)
        }
        if false == strings.HasPrefix(refused.headerList.Get("Content-Type"), "application/json") || false == json.Valid([]byte(refused.body)) {
            fail("[%s] the 400 under Accept %q is not the default json body (%q): %s", major.label, accept, refused.headerList.Get("Content-Type"), exampleTruncate(refused.body))
        }
    }
    pass("[%s] an Accept refusing every media type keeps the kernel's 400 and its json body, as accepting json does", major.label)
}
