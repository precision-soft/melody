package security

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    "encoding/hex"
    "net/http/httptest"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
    nethttp "net/http"
    "crypto/sha256"
    "testing"
    "time"
)

func plainRequest(t *testing.T) melodyhttpcontract.Request {
    t.Helper()

    return melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/products/", nil), nil, nil, nil)
}

func requestCarryingSession(t *testing.T) (melodyhttpcontract.Request, melodysessioncontract.Session) {
    t.Helper()

    request := plainRequest(t)

    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)
    sessionInstance := manager.NewSession()

    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    return request, sessionInstance
}

func requestCarryingSessionAttribute(t *testing.T, value any) melodyhttpcontract.Request {
    t.Helper()

    request := plainRequest(t)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, value)

    return request
}

func testSessionUserLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    roles := []string{"ROLE_USER", "ROLE_EDITOR"}
    if "user-3" == userId {
        roles = []string{"ROLE_USER", "ROLE_ADMIN"}
    }
    return entity.NewUser(userId, "test-user", "test-password-hash", roles), true, nil
}

func storedValueBcryptCannotRead(plaintextPassword string) string {
    digest := sha256.Sum256([]byte(plaintextPassword))

    return hex.EncodeToString(digest[:])
}

const equalizedRefusalFloor = 5 * time.Millisecond

func signedIn(t *testing.T, userId string, roles []string) melodysecuritycontract.Token {
    t.Helper()

    request, sessionInstance := requestCarryingSession(t)
    sessionInstance.Set(SessionKeySecurityUserId, userId)
    sessionInstance.Set(SessionKeySecurityCredentialVersion, SessionCredentialVersion("test-password-hash"))
    sessionInstance.Set(SessionKeySecurityRoles, roles)

    return SessionTokenResolver(testSessionUserLookup)(request)
}
