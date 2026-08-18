package security

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodysession "github.com/precision-soft/melody/v2/session"
    melodysessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

/* The shared test material of the package lives here, and only here: this is the one test file the layout rule exempts from having a source of its own. */

/* requestAccepting builds a request carrying the Accept header the caller names, and no header at all for the empty string. The runtime and the request context are left nil because nothing on the paths under test reads them — the entry point, the access denied handler and the token resolver all reach the underlying http request or the attribute bag. */
func requestAccepting(t *testing.T, acceptHeader string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    if "" != acceptHeader {
        httpRequest.Header.Set("Accept", acceptHeader)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, nil)
}

/* requestAcceptingLines is the same, for a client that sent the Accept field as SEVERAL header lines rather than as one comma-joined list. Both spellings are legal and mean the same thing, which is the whole point of the assertions that use it. */
func requestAcceptingLines(t *testing.T, acceptLineList ...string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    for _, acceptLine := range acceptLineList {
        httpRequest.Header.Add("Accept", acceptLine)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, nil)
}

/* requestAtPathWithHeader builds a request at the path the caller names, carrying one header when the value is not empty — the two coordinates the api-key matcher reads. */
func requestAtPathWithHeader(t *testing.T, path string, headerName string, headerValue string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, path, nil)
    if "" != headerValue {
        httpRequest.Header.Set(headerName, headerValue)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, nil)
}

/* requestCarryingSession hands back a request whose attribute bag holds the session, the way the http kernel publishes it, together with the session itself so the caller can write into it. A real session is used rather than a double: the token resolver reads the values back through the typed getters, and a double would let the test agree with itself about what a session stores. */
func requestCarryingSession(t *testing.T) (melodyhttpcontract.Request, melodysessioncontract.Session) {
    t.Helper()

    request := requestAccepting(t, "")

    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)
    sessionInstance := manager.NewSession()

    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    return request, sessionInstance
}

/* requestCarryingSessionAttribute publishes an arbitrary value under the session attribute, so the resolver can be asked what it does when something that is NOT a session sits where one is expected. */
func requestCarryingSessionAttribute(t *testing.T, value any) melodyhttpcontract.Request {
    t.Helper()

    request := requestAccepting(t, "")
    request.Attributes().Set(melodyhttp.RequestAttributeSession, value)

    return request
}
