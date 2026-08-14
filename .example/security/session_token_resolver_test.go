package security

import (
    "testing"

    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

/* signedIn writes the pair the login handler writes, which is the only shape the resolver accepts. */
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

/* A file-backed session storage snapshots through json and answers the role list as []any; the resolver accepts that spelling when every element is a string, which is what keeps a signed-in session signed in across a process restart. */
func TestSessionTokenResolverAcceptsARestoredRoleList(t *testing.T) {
    request, sessionInstance := requestCarryingSession(t)
    sessionInstance.Set(SessionKeySecurityUserId, "user-2")
    sessionInstance.Set(SessionKeySecurityRoles, []any{"ROLE_USER", "ROLE_EDITOR"})

    token := SessionTokenResolver()(request)

    if false == token.IsAuthenticated() {
        t.Fatalf("a restored role list left the request anonymous")
    }

    if 2 != len(token.Roles()) || "ROLE_USER" != token.Roles()[0] || "ROLE_EDITOR" != token.Roles()[1] {
        t.Fatalf("unexpected roles: %v", token.Roles())
    }
}

/* Every guard below answers the SAME anonymous token, so each is driven on its own: a table that fused them would let one guard cover for a sibling that had been disarmed. Together they are the whole fail-closed surface between a request and an identity — anything the session cannot supply exactly leaves the request anonymous rather than partly authenticated. */
func TestSessionTokenResolverFailsClosed(t *testing.T) {
    for name, request := range map[string]func(*testing.T) melodysecuritycontract.Token{
        "no request at all": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver()(nil)
        },
        "no session published on the request": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver()(requestAccepting(t, ""))
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
        "roles that are not a string slice": func(t *testing.T) melodysecuritycontract.Token {
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
            token := request(t)

            if nil == token {
                t.Fatalf("the resolver answered no token at all")
            }

            if true == token.IsAuthenticated() {
                t.Fatalf("the request was authenticated as %q with roles %v", token.UserIdentifier(), token.Roles())
            }
        })
    }
}
