package security

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    melodyclock "github.com/precision-soft/melody/clock"
    melodyclockcontract "github.com/precision-soft/melody/clock/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodysession "github.com/precision-soft/melody/session"
    melodysessioncontract "github.com/precision-soft/melody/session/contract"
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

/* sessionAdmission is one call the sign-in doors made on the session index */
type sessionAdmission struct {
    userId            string
    previousSessionId string
    sessionId         string
    createdAt         time.Time
}

/* recordingSessionIndex stands in for repository.UserSessionRepository, which this package cannot import: it records what the doors ask of it, and refuses every admission with admitErr when one is set */
type recordingSessionIndex struct {
    admitErr     error
    admittedList []sessionAdmission
    releasedList []string
}

func (instance *recordingSessionIndex) Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, release func(sessionId string) error) error {
    if nil != instance.admitErr {
        return instance.admitErr
    }

    instance.admittedList = append(instance.admittedList, sessionAdmission{userId: userId, previousSessionId: previousSessionId, sessionId: sessionId, createdAt: createdAt})

    return nil
}

func (instance *recordingSessionIndex) Release(ctx context.Context, sessionId string) error {
    instance.releasedList = append(instance.releasedList, sessionId)

    return nil
}

func (instance *recordingSessionIndex) lookup(request melodyhttpcontract.Request) (SessionIndex, error) {
    return instance, nil
}

/* sessionAdmissionTestInstant is the frozen clock's instant an admission records */
var sessionAdmissionTestInstant = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

/* registerAdmissionClock gives the container the clock AdmitSession stamps an admission with */
func registerAdmissionClock(t *testing.T, containerInstance melodycontainercontract.Container) {
    t.Helper()

    registerErr := melodycontainer.Register[melodyclockcontract.Clock](
        containerInstance,
        melodyclock.ServiceClock,
        func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
            return melodyclock.NewFrozenClock(sessionAdmissionTestInstant), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register clock: %v", registerErr)
    }
}
