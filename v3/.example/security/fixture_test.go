package security

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
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

const testPasswordHash = "test-password-hash"

/* currentAccountLookup answers every account as present, holding testPasswordHash and the roles the session tests write for it, so a resolution that turns anonymous is one of the session's own guards and not the account check. */
func currentAccountLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    roles := []string{"ROLE_USER", "ROLE_EDITOR"}
    if "user-3" == userId {
        roles = []string{"ROLE_USER", "ROLE_ADMIN"}
    }

    return entity.NewUser(userId, "test-user", testPasswordHash, roles), true, nil
}

var errAccountRepositoryUnavailable = errors.New("the account repository is unavailable")

func refusingAccountLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    return nil, false, errAccountRepositoryUnavailable
}

/* withCurrentCredential writes the credential version of testPasswordHash, so a test that drives an earlier guard is not also refused for a missing version */
func withCurrentCredential(sessionInstance melodysessioncontract.Session) {
    sessionInstance.Set(SessionKeySecurityCredentialVersion, SessionCredentialVersion(testPasswordHash))
}

/* passwordRequest is a request carrying the credentials the sign-in door sets; an empty one is left unset. */
func passwordRequest(t *testing.T, username string, password string) melodyhttpcontract.Request {
    t.Helper()

    request := plainRequest(t)
    if "" != username {
        request.Attributes().Set(RequestAttributeLoginUsername, username)
    }
    if "" != password {
        request.Attributes().Set(RequestAttributeLoginPassword, password)
    }

    return request
}

const budgetTestAllowance = 2

/* budgetOverPassword is the budget over a password check that accepts "secret" for the editor and refuses anything else. */
func budgetOverPassword() *SecondFactorBudget {
    password := NewPasswordAuthenticator(func(runtimeInstance melodyruntimecontract.Runtime, username string, password string) (*entity.User, bool, error) {
        if "secret" != password {
            return nil, false, nil
        }

        return entity.NewUser("user-"+username, username, testPasswordHash, []string{"ROLE_EDITOR"}), true, nil
    })

    limiter := melodyhttpmiddleware.NewSlidingWindowLimiterWithClock(melodyclock.NewFrozenClock(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)), budgetTestAllowance, time.Minute)

    return NewSecondFactorBudget(password, limiter, melodysecurity.DefaultTotpCodeHeaderName, melodysecurity.DefaultTotpRecoveryHeaderName)
}

func budgetRequestWithHeader(t *testing.T, password string, headerName string) melodyhttpcontract.Request {
    t.Helper()

    request := passwordRequest(t, "editor", password)
    if "" != headerName {
        request.HttpRequest().Header.Set(headerName, "123456")
    }

    return request
}
