package security

import (
    "path/filepath"
    "testing"
    "time"

    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodysession "github.com/precision-soft/melody/v3/session"
)

/* signedIn writes the pair the login handler writes. */
func signedIn(t *testing.T, userId string, roles []string) melodysecuritycontract.Token {
    t.Helper()

    request, sessionInstance := requestCarryingSession(t)
    sessionInstance.Set(SessionKeySecurityUserId, userId)
    sessionInstance.Set(SessionKeySecurityRoles, roles)

    return SessionTokenResolver()(request)
}

func TestSessionTokenResolverAnswersTheSignedInIdentity(t *testing.T) {
    token := signedIn(t, "user-3", []string{"ROLE_USER", "ROLE_ADMIN"})

    if false == token.IsAuthenticated() {
        t.Fatalf("a session carrying an identity resolved to an anonymous token")
    }

    if "user-3" != token.UserIdentifier() {
        t.Fatalf("unexpected user id: %q", token.UserIdentifier())
    }

    if 2 != len(token.Roles()) {
        t.Fatalf("unexpected roles: %v", token.Roles())
    }
}

/* The producer is driven rather than imitated: a real file-backed storage is written, closed and read again by a second storage, which is what a process restart is. The role list comes back as []any because the snapshot round-trips through json — session.FileStorage says so in its own GoDoc — and a resolver that accepted only []string would leave every signed-in visitor anonymous from the first restart onwards, on the very storage the boot warning asks an operator to register. */
func TestSessionTokenResolverKeepsAnIdentityAcrossARestart(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage, err := melodysession.NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("the file storage was not built: %v", err)
    }

    manager := melodysession.NewManager(storage, time.Hour)
    sessionInstance := manager.NewSession()
    sessionInstance.Set(SessionKeySecurityUserId, "user-7")
    sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER", "ROLE_EDITOR"})

    if err := manager.SaveSession(sessionInstance); nil != err {
        t.Fatalf("the session was not saved: %v", err)
    }

    sessionId := sessionInstance.Id()

    if err := storage.Close(); nil != err {
        t.Fatalf("the storage did not close: %v", err)
    }

    restartedStorage, err := melodysession.NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("the restarted file storage was not built: %v", err)
    }
    defer restartedStorage.Close()

    restoredSession := melodysession.NewManager(restartedStorage, time.Hour).Session(sessionId)
    if nil == restoredSession {
        t.Fatal("the session did not survive the restart at all")
    }

    if _, isStringSlice := restoredSession.Get(SessionKeySecurityRoles).([]string); true == isStringSlice {
        t.Fatal("the storage kept the element type, so this test no longer drives the shape it exists for")
    }

    request := plainRequest(t)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, restoredSession)

    token := SessionTokenResolver()(request)

    if false == token.IsAuthenticated() {
        t.Fatal("a session restored from a file-backed storage left the request anonymous")
    }

    if "user-7" != token.UserIdentifier() {
        t.Fatalf("unexpected user id: %q", token.UserIdentifier())
    }

    if 2 != len(token.Roles()) || "ROLE_USER" != token.Roles()[0] || "ROLE_EDITOR" != token.Roles()[1] {
        t.Fatalf("unexpected roles: %v", token.Roles())
    }
}

/* the same shape, written directly, so the acceptance is pinned without a filesystem under it */
func TestSessionTokenResolverAcceptsARestoredRoleList(t *testing.T) {
    request, sessionInstance := requestCarryingSession(t)
    sessionInstance.Set(SessionKeySecurityUserId, "user-2")
    sessionInstance.Set(SessionKeySecurityRoles, []any{"ROLE_USER", "ROLE_EDITOR"})

    token := SessionTokenResolver()(request)

    if false == token.IsAuthenticated() {
        t.Fatal("a restored role list left the request anonymous")
    }

    if 2 != len(token.Roles()) || "ROLE_USER" != token.Roles()[0] || "ROLE_EDITOR" != token.Roles()[1] {
        t.Fatalf("unexpected roles: %v", token.Roles())
    }
}

/* Every guard below answers the SAME anonymous token, so each is driven on its own: a table that fused them would let one guard cover for a sibling that had been disarmed. Together they are the whole fail-closed surface between a request and an identity — anything the session cannot supply exactly leaves the request anonymous rather than partly authenticated. */
func TestSessionTokenResolverFailsClosed(t *testing.T) {
    for name, resolve := range map[string]func(*testing.T) melodysecuritycontract.Token{
        "no request at all": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver()(nil)
        },
        "no session published on the request": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver()(plainRequest(t))
        },
        "something that is not a session under the session attribute": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver()(requestCarryingSessionAttribute(t, "not a session"))
        },
        "a session with no user id": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER"})

            return SessionTokenResolver()(request)
        },
        "a user id that is not a string": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, 7)
            sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER"})

            return SessionTokenResolver()(request)
        },
        "an empty user id": func(t *testing.T) melodysecuritycontract.Token {
            return signedIn(t, "", []string{"ROLE_USER"})
        },
        "a session with no roles": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")

            return SessionTokenResolver()(request)
        },
        "roles that are neither spelling": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")
            sessionInstance.Set(SessionKeySecurityRoles, "ROLE_USER")

            return SessionTokenResolver()(request)
        },
        "an empty role list": func(t *testing.T) melodysecuritycontract.Token {
            return signedIn(t, "user-1", []string{})
        },
        "a restored role list carrying a non-string": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")
            sessionInstance.Set(SessionKeySecurityRoles, []any{"ROLE_USER", 7})

            return SessionTokenResolver()(request)
        },
        "an empty restored role list": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")
            sessionInstance.Set(SessionKeySecurityRoles, []any{})

            return SessionTokenResolver()(request)
        },
    } {
        t.Run(name, func(t *testing.T) {
            token := resolve(t)

            if nil == token {
                t.Fatal("the resolver answered no token at all")
            }

            if true == token.IsAuthenticated() {
                t.Fatalf("the request was authenticated as %q with roles %v", token.UserIdentifier(), token.Roles())
            }
        })
    }
}
