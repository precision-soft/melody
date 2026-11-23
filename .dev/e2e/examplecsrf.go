package main

import (
    "encoding/json"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "regexp"
    "time"
)

const (
    exampleCsrfTokenRoute  = "/csrf/token/"
    exampleCsrfHeaderName  = "X-CSRF-Token"
    exampleCsrfFieldName   = "_csrf_token"
    exampleProductListPage = "/products/"
)

var exampleCsrfMetaPattern = regexp.MustCompile(`<meta content="([A-Za-z0-9_-]+)" name="csrf-token"/>`)

/* newExampleClientStatingNothing is a cookie jar that sends neither Origin nor Sec-Fetch-Site, so the forgery check falls to the token */
func newExampleClientStatingNothing(major exampleMajor) *exampleClient {
    jar, jarErr := cookiejar.New(nil)
    if nil != jarErr {
        fail("[%s] csrf: open a cookie jar: %v", major.label, jarErr)
    }

    return &exampleClient{
        major: major,
        jar:   jar,
        client: &http.Client{
            Timeout: 10 * time.Second,
            Jar:     jar,
            CheckRedirect: func(request *http.Request, previous []*http.Request) error {
                return http.ErrUseLastResponse
            },
        },
    }
}

func exampleCsrfTokenFromRoute(major exampleMajor, client *exampleClient) string {
    answer := client.call("GET", exampleCsrfTokenRoute, "application/json", "", "")
    if http.StatusOK != answer.statusCode || "no-store" != answer.headerList.Get("Cache-Control") {
        fail("[%s] GET %s answered %d with Cache-Control %q, wanted 200 no-store: %s", major.label, exampleCsrfTokenRoute, answer.statusCode, answer.headerList.Get("Cache-Control"), exampleTruncate(answer.body))
    }

    payload := struct {
        Token string `json:"token"`
    }{}
    if decodeErr := json.Unmarshal([]byte(answer.body), &payload); nil != decodeErr || "" == payload.Token {
        fail("[%s] GET %s answered no token: %s", major.label, exampleCsrfTokenRoute, exampleTruncate(answer.body))
    }

    return payload.Token
}

/* the main firewall checks every write riding the session: a create carrying no token, a token of the session before the sign-in, or a valid token sent from a sibling or a foreign origin is refused 403, where a create carrying the token of the token route or of the page's meta reaches the validation's 400, and the sign-out form's field is read. The creates write nothing */
func assertExampleCsrf(major exampleMajor) {
    client := newExampleClientStatingNothing(major)
    emptyCreate := `{"name":""}`

    beforeSignIn := exampleCsrfTokenFromRoute(major, client)

    signIn := client.call("POST", exampleSignInRoute(major), "application/json", "application/json", exampleCredentialBody(exampleEditorUsername, exampleEditorPassword))
    if http.StatusOK != signIn.statusCode {
        fail("[%s] the seeded editor could not sign in (%d) stating no origin: %s", major.label, signIn.statusCode, exampleTruncate(signIn.body))
    }

    if missing := client.call("POST", exampleProductCreateRoute, "application/json", "application/json", emptyCreate); http.StatusForbidden != missing.statusCode {
        fail("[%s] a create carrying no token answered %d, wanted 403: %s", major.label, missing.statusCode, exampleTruncate(missing.body))
    }

    if stale := client.callWithHeaderList("POST", exampleProductCreateRoute, "application/json", "application/json", map[string]string{exampleCsrfHeaderName: beforeSignIn}, emptyCreate); http.StatusForbidden != stale.statusCode {
        fail("[%s] a create carrying the token issued before the sign-in answered %d, wanted 403: %s", major.label, stale.statusCode, exampleTruncate(stale.body))
    }

    routeToken := exampleCsrfTokenFromRoute(major, client)
    if fromRoute := client.callWithHeaderList("POST", exampleProductCreateRoute, "application/json", "application/json", map[string]string{exampleCsrfHeaderName: routeToken}, emptyCreate); http.StatusBadRequest != fromRoute.statusCode {
        fail("[%s] a create carrying the token route's token answered %d, wanted the validation's 400: %s", major.label, fromRoute.statusCode, exampleTruncate(fromRoute.body))
    }

    page := client.call("GET", exampleProductListPage, "text/html", "", "")
    metaMatch := exampleCsrfMetaPattern.FindStringSubmatch(page.body)
    if http.StatusOK != page.statusCode || 2 != len(metaMatch) {
        fail("[%s] GET %s answered %d without the csrf meta", major.label, exampleProductListPage, page.statusCode)
    }

    if fromMeta := client.callWithHeaderList("POST", exampleProductCreateRoute, "application/json", "application/json", map[string]string{exampleCsrfHeaderName: metaMatch[1]}, emptyCreate); http.StatusBadRequest != fromMeta.statusCode {
        fail("[%s] a create carrying the page's token answered %d, wanted the validation's 400: %s", major.label, fromMeta.statusCode, exampleTruncate(fromMeta.body))
    }

    for name, headerList := range map[string]map[string]string{
        "a sibling subdomain": {exampleCsrfHeaderName: routeToken, "Sec-Fetch-Site": "same-site"},
        "a foreign origin":    {exampleCsrfHeaderName: routeToken, "Origin": "https://evil.example"},
    } {
        if forged := client.callWithHeaderList("POST", exampleProductCreateRoute, "application/json", "application/json", headerList, emptyCreate); http.StatusForbidden != forged.statusCode {
            fail("[%s] a create with a valid token from %s answered %d, wanted 403: %s", major.label, name, forged.statusCode, exampleTruncate(forged.body))
        }
    }

    loggedOut := client.call("POST", exampleLogoutRoute, "text/html", "application/x-www-form-urlencoded", exampleCsrfFieldName+"="+url.QueryEscape(metaMatch[1]))
    if http.StatusFound != loggedOut.statusCode {
        fail("[%s] the sign-out form carrying the field answered %d, wanted the door's 302: %s", major.label, loggedOut.statusCode, exampleTruncate(loggedOut.body))
    }

    pass("[%s] a write riding the session is refused 403 without a token, with a stale one or from another origin, and served with the token of the token route, of the page or of the sign-out form's field", major.label)
}
