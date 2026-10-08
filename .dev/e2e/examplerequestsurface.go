package main

import (
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "strings"
)

/* exampleRequestSurfaceClaimedRequestId is the identifier the probe claims on its own request. The kernel mints the one a response is filed under, so finding this value in an envelope can only mean the example echoed what the client sent. */
const exampleRequestSurfaceClaimedRequestId = "e2e-claimed-request-id"

/* runExampleRequestSurfaceAssertions drives the request surface the example serves beyond the shared sections, anonymously: a path spelled other than canonically is refused before the firewall reads it, the method doors answer HEAD, OPTIONS and a 405 with the methods the route allows, the error envelope names the request the kernel filed, the static surface serves no dotfile and a malformed session cookie is an anonymous caller rather than a failure. */
func runExampleRequestSurfaceAssertions(major exampleMajor, application *exampleApplication) {
    assertExampleNonCanonicalPathsAreRefusedBeforeTheFirewall(major)
    assertExampleMethodDoors(major)
    assertExampleErrorEnvelopeCarriesTheMintedRequestId(major)
    assertExampleStaticSurfaceServesNoDotfile(major)
    assertExampleMalformedSessionCookieIsAnonymous(major)
    assertExampleRefusingAcceptKeepsTheErrorStatus(major)
    assertExampleRequestBodyLimit(major)
    assertExampleExcludedStaticPaths(major, application)
    assertExampleOnePathDoorsArePublicForTheirOwnSpelling(major)
    assertExampleSessionCookieFollowsTheForwardedScheme(major)
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

        page := client.callWithHeaderList("GET", exampleMissingRoute, "text/html", "", headerList, "")
        if http.StatusNotFound != page.statusCode {
            fail("[%s] %s answered %d to a browser, wanted 404", major.label, exampleMissingRoute, page.statusCode)
        }

        pageMinted := page.headerList.Get("X-Request-Id")
        if "" == pageMinted || pageMinted == claimed {
            fail("[%s] the html 404 carries X-Request-Id %q, wanted one the kernel minted (claimed %q)", major.label, pageMinted, claimed)
        }
        if false == strings.Contains(page.body, "Reference: <code>"+pageMinted+"</code>") {
            fail(
                "[%s] the html 404 page does not name the request %q its response header carries as its reference (the request claimed %q): %s",
                major.label,
                pageMinted,
                claimed,
                exampleTruncate(page.body),
            )
        }
    }
    pass("[%s] the 404 envelope and the html 404 page both name the request id the response header carries, the kernel's, with and without a client claim", major.label)
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

/* an Accept that refuses every media type does not turn an error the framework renders into a 406: the error's own status is the signal, so it is kept and served the default json body. The door is the canonical-path refusal, which the kernel renders itself — the example's firewall answers its own errors in json and its presenter keeps a refusal's status on a refusing Accept by its own rule. The control is the same request accepting json; the refusing arm spells the refusal both ways, every registered type at q=0 and the wildcard at q=0 */
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

/* a client that negotiates text/plain reads an error as lines on every major, both where the kernel renders it (the canonical-path refusal and the 405 of a wrong method, which keeps its Allow) and where the example's presenter does (the not-found handler behind the public asset prefix): the plain-text serializer handed the envelope printed a Go map or the envelope's fields bare. The control is the same two requests accepting json, answered by a json body */
func assertExampleTextPlainErrorsAreReadable(major exampleMajor) {
    client := newExampleClient(major)

    doors := []struct {
        method     string
        path       string
        statusCode int
        statusLine string
    }{
        {method: "GET", path: "/" + exampleUserRoute, statusCode: http.StatusBadRequest, statusLine: "400 bad request"},
        {method: "GET", path: "/assets/zz-e2e-missing.js", statusCode: http.StatusNotFound, statusLine: "404 not found"},
        {method: "POST", path: "/health", statusCode: http.StatusMethodNotAllowed, statusLine: "405 method not allowed"},
    }

    for _, door := range doors {
        control := client.call(door.method, door.path, "application/json", "", "")
        if door.statusCode != control.statusCode || false == json.Valid([]byte(control.body)) {
            fail("[%s] %s accepting json answered %d %s, wanted %d with a json body as the control", major.label, door.path, control.statusCode, exampleTruncate(control.body), door.statusCode)
        }

        answered := client.call(door.method, door.path, "text/plain", "", "")
        if door.statusCode != answered.statusCode || false == strings.HasPrefix(answered.headerList.Get("Content-Type"), "text/plain") {
            fail("[%s] %s accepting text/plain answered %d %q, wanted %d text/plain", major.label, door.path, answered.statusCode, answered.headerList.Get("Content-Type"), door.statusCode)
        }

        if http.StatusMethodNotAllowed == door.statusCode && (false == strings.Contains(answered.headerList.Get("Allow"), "GET") || false == strings.Contains(control.headerList.Get("Allow"), "GET")) {
            fail("[%s] %s %s lost its Allow: text/plain %q, json %q", major.label, door.method, door.path, answered.headerList.Get("Allow"), control.headerList.Get("Allow"))
        }

        lines := strings.Split(strings.TrimSuffix(answered.body, "\n"), "\n")
        if door.statusLine != lines[0] || true == strings.Contains(answered.body, "map[") || true == strings.Contains(answered.body, "{false") {
            fail("[%s] %s accepting text/plain is not written as lines starting %q: %s", major.label, door.path, door.statusLine, exampleTruncate(answered.body))
        }

        requestIdLine := "request id: " + answered.headerList.Get("X-Request-Id")
        timeLines := 0
        requestIdLines := 0
        for _, line := range lines {
            if true == strings.HasPrefix(line, "time: ") {
                timeLines++
            }
            if requestIdLine == line {
                requestIdLines++
            }
        }
        if 1 != timeLines {
            fail("[%s] %s accepting text/plain carries %d time lines, wanted one: %s", major.label, door.path, timeLines, exampleTruncate(answered.body))
        }

        /* the kernel's envelope names the request id on the third major only; the presenter's context names it on every major */
        wantsRequestId := 3 == major.number || http.StatusNotFound == door.statusCode
        if true == wantsRequestId && 1 != requestIdLines {
            fail("[%s] %s accepting text/plain does not name the request id %q of its response header: %s", major.label, door.path, requestIdLine, exampleTruncate(answered.body))
        }
    }
    pass("[%s] a text/plain client reads the kernel's 400, the kernel's 405 with its Allow and the presenter's 404 as lines naming the status, the time and the request id, where json clients read json", major.label)
}

/* the example bounds every request body at 64 KiB (MELODY_HTTP_MAX_REQUEST_BODY_BYTES): a product create past it is refused 413 in the envelope before the door binds it, where a body just under it reaches the validation and is refused 400 for the empty name. The control writes nothing, so the section spends no write of the nomenclature */
func assertExampleRequestBodyLimit(major exampleMajor) {
    editor := newExampleClient(major)

    signIn := editor.call("POST", exampleLoginRoute, "application/json", "application/json", exampleCredentialBody(exampleEditorUsername, exampleEditorPassword))
    if http.StatusOK != signIn.statusCode {
        fail("[%s] the seeded editor could not sign in (%d) to drive the body limit: %s", major.label, signIn.statusCode, exampleTruncate(signIn.body))
    }

    underLimit := `{"name":"","color":"` + strings.Repeat("x", 60*1024) + `"}`
    control := editor.call("POST", exampleProductCreateRoute, "application/json", "application/json", underLimit)
    if http.StatusBadRequest != control.statusCode {
        fail("[%s] a %d-byte create under the body limit answered %d, wanted the validation's 400 as the control: %s", major.label, len(underLimit), control.statusCode, exampleTruncate(control.body))
    }

    pastLimit := `{"name":"","color":"` + strings.Repeat("x", 128*1024) + `"}`
    refused := editor.call("POST", exampleProductCreateRoute, "application/json", "application/json", pastLimit)
    if http.StatusRequestEntityTooLarge != refused.statusCode || false == strings.Contains(refused.body, "payload too large") {
        fail("[%s] a %d-byte create past the body limit answered %d, wanted 413 payload too large: %s", major.label, len(pastLimit), refused.statusCode, exampleTruncate(refused.body))
    }
    pass("[%s] a create past the 64 KiB body limit is refused 413 payload too large, where one just under it reaches the validation's 400", major.label)
}

/* the file server answers ahead of routing, behind the firewall, so a file under public/ named like one of the application's doors would be served to a signed-in caller in place of the door. The example reserves its doors' prefixes (MELODY_STATIC_EXCLUDED_PATHS), so a file planted in the workspace's public/storage/object is not served on /storage/object: the storage door answers, or the not-found handler where no object store is wired, but never the planted content. The control is the workspace's own public/test.txt, still served, so the static surface is on */
func assertExampleExcludedStaticPaths(major exampleMajor, application *exampleApplication) {
    const plantedContent = "planted-e2e-static-shadow"

    plantedPath := filepath.Join(filepath.Dir(application.logPath), "public", "storage", "object")
    if mkdirErr := os.MkdirAll(filepath.Dir(plantedPath), 0o755); nil != mkdirErr {
        fail("[%s] plant a file under the workspace's public/storage: %v", major.label, mkdirErr)
    }
    if writeErr := os.WriteFile(plantedPath, []byte(plantedContent), 0o644); nil != writeErr {
        fail("[%s] plant a file under the workspace's public/storage: %v", major.label, writeErr)
    }
    defer func() {
        _ = os.RemoveAll(filepath.Dir(plantedPath))
    }()

    editor := newExampleClient(major)
    signIn := editor.call("POST", exampleLoginRoute, "application/json", "application/json", exampleCredentialBody(exampleEditorUsername, exampleEditorPassword))
    if http.StatusOK != signIn.statusCode {
        fail("[%s] the seeded editor could not sign in (%d) to drive the excluded static paths: %s", major.label, signIn.statusCode, exampleTruncate(signIn.body))
    }

    control := editor.call("GET", "/test.txt", "", "", "")
    if http.StatusOK != control.statusCode {
        fail("[%s] /test.txt answered %d, wanted the static surface's 200 as the control", major.label, control.statusCode)
    }

    shadowed := editor.call("GET", "/storage/object?key=zz-e2e-absent", "application/json", "", "")
    if true == strings.Contains(shadowed.body, plantedContent) || false == json.Valid([]byte(shadowed.body)) {
        fail("[%s] /storage/object answered %d with the file planted under public/ instead of the door: %s", major.label, shadowed.statusCode, exampleTruncate(shadowed.body))
    }
    pass("[%s] a file planted under public/storage is not served in place of the /storage door (answered %d in json), while /test.txt is still served", major.label, shadowed.statusCode)
}

/* the example opens a door that is one path with an exact rule: /health and /openapi.json are public, and a neighbouring spelling a prefix rule would have opened — /healthz, /openapiXjson, where the unescaped dot of a regex matched any character — falls to the catch-all and is refused an anonymous caller */
func assertExampleOnePathDoorsArePublicForTheirOwnSpelling(major exampleMajor) {
    client := newExampleClient(major)

    for _, path := range []string{exampleHealthRoute, "/openapi.json"} {
        control := client.call("GET", path, "application/json", "", "")
        if http.StatusOK != control.statusCode {
            fail("[%s] %s answered %d to an anonymous caller, wanted its public 200 as the control", major.label, path, control.statusCode)
        }
    }

    for _, path := range []string{"/healthz", "/health/extra", "/openapiXjson"} {
        refused := client.call("GET", path, "application/json", "", "")
        if http.StatusUnauthorized != refused.statusCode {
            fail("[%s] %s answered %d to an anonymous caller, wanted the catch-all's 401: a prefix rule opened it", major.label, path, refused.statusCode)
        }
    }
    pass("[%s] /health and /openapi.json are public, and /healthz, /health/extra and /openapiXjson are refused 401 by the catch-all", major.label)
}

/* the example trusts the forwarded scheme from the private ranges and loopback, the harness's own peer: a sign-in that arrives with X-Forwarded-Proto: https, as a proxy that terminates tls forwards it, is answered with a Secure session cookie, and the same sign-in without the header is not, which is the control that the header, read from a trusted peer, decides it */
func assertExampleSessionCookieFollowsTheForwardedScheme(major exampleMajor) {
    sessionCookieOf := func(headerList map[string]string) string {
        response := newExampleClient(major).callWithHeaderList("POST", exampleLoginRoute, "application/json", "application/json", headerList, exampleCredentialBody(exampleUsername, examplePassword))
        if http.StatusOK != response.statusCode {
            fail("[%s] the seeded user could not sign in (%d) to read the session cookie: %s", major.label, response.statusCode, exampleTruncate(response.body))
        }

        return strings.Join(response.headerList.Values("Set-Cookie"), "; ")
    }

    forwarded := sessionCookieOf(map[string]string{"X-Forwarded-Proto": "https"})
    direct := sessionCookieOf(map[string]string{})

    if false == strings.Contains(forwarded, "Secure") || true == strings.Contains(direct, "Secure") {
        fail("[%s] the session cookie does not follow the forwarded scheme: with X-Forwarded-Proto https %q, without it %q", major.label, forwarded, direct)
    }
    pass("[%s] a sign-in forwarded as https gets a Secure session cookie and a plain one does not", major.label)
}

/* the sign-in door reads json alone, the one body a cross-site form cannot post: the seeded user's own credentials posted as a form are refused 415 and open no session, and the same credentials as json sign in, the control that the media type, not the credentials, decided it */
func assertExampleSignInRefusesAFormBody(major exampleMajor, redisAddress string) {
    /* both sign-ins spend the per-address write budget the sections before this one already spent */
    if "" != redisAddress {
        label := "[" + major.label + "] form sign-in"
        resetExampleRateLimitCounters(label, redisAddress, exampleMajorRateLimitPrefix(major))
        defer resetExampleRateLimitCounters(label, redisAddress, exampleMajorRateLimitPrefix(major))
    }

    form := newExampleClient(major).call("POST", exampleLoginRoute, "application/json", "application/x-www-form-urlencoded", "username="+exampleUsername+"&password="+examplePassword)
    if http.StatusUnsupportedMediaType != form.statusCode || 0 != len(form.headerList.Values("Set-Cookie")) {
        fail("[%s] a form-encoded sign-in answered %d with cookies %v, wanted 415 and no session: %s", major.label, form.statusCode, form.headerList.Values("Set-Cookie"), exampleTruncate(form.body))
    }

    signedIn := newExampleClient(major).call("POST", exampleLoginRoute, "application/json", "application/json", exampleCredentialBody(exampleUsername, examplePassword))
    if http.StatusOK != signedIn.statusCode {
        fail("[%s] the same credentials as json answered %d: %s", major.label, signedIn.statusCode, exampleTruncate(signedIn.body))
    }
    pass("[%s] a form-encoded sign-in is refused 415 without a session and the same credentials as json sign in", major.label)
}
