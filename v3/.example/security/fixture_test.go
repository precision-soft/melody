package security

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

/* The shared test material of the package lives here, and only here: this is the one test file the layout rule exempts from having a source of its own. */

/* plainRequest builds a request with no headers of its own. The runtime and the request context are left nil because nothing on the paths under test reads them — the token resolver reaches the attribute bag and nothing else. */
func plainRequest(t *testing.T) melodyhttpcontract.Request {
    t.Helper()

    return melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/products/", nil), nil, nil, nil)
}

/* requestCarryingSession hands back a request whose attribute bag holds the session, the way the http kernel publishes it, together with the session itself so the caller can write into it. A real session is used rather than a double: the resolver reads the values back through the typed getters, and a double would let the test agree with itself about what a session stores. */
func requestCarryingSession(t *testing.T) (melodyhttpcontract.Request, melodysessioncontract.Session) {
    t.Helper()

    request := plainRequest(t)

    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)
    sessionInstance := manager.NewSession()

    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    return request, sessionInstance
}

/* requestCarryingSessionAttribute publishes an arbitrary value under the session attribute, so the resolver can be asked what it does when something that is NOT a session sits where one is expected. */
func requestCarryingSessionAttribute(t *testing.T, value any) melodyhttpcontract.Request {
    t.Helper()

    request := plainRequest(t)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, value)

    return request
}
