package security

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodylogging "github.com/precision-soft/melody/logging"
    melodyloggingcontract "github.com/precision-soft/melody/logging/contract"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
    melodysession "github.com/precision-soft/melody/session"
    melodysessioncontract "github.com/precision-soft/melody/session/contract"
)

/* signedIn writes what the login handler writes: the identity, the roles and the credential version. */
func signedIn(t *testing.T, userId string, roles []string) melodysecuritycontract.Token {
    t.Helper()

    request, sessionInstance := requestCarryingSession(t)
    sessionInstance.Set(SessionKeySecurityUserId, userId)
    sessionInstance.Set(SessionKeySecurityRoles, roles)
    withCurrentCredential(sessionInstance)

    return SessionTokenResolver(currentAccountLookup)(request)
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
    withCurrentCredential(sessionInstance)

    token := SessionTokenResolver(currentAccountLookup)(request)

    if false == token.IsAuthenticated() {
        t.Fatalf("a restored role list left the request anonymous")
    }

    if 2 != len(token.Roles()) || "ROLE_USER" != token.Roles()[0] || "ROLE_EDITOR" != token.Roles()[1] {
        t.Fatalf("unexpected roles: %v", token.Roles())
    }
}

/* Every guard below answers the SAME anonymous token, so each is driven on its own: a table that fused them would let one guard cover for a disarmed sibling. Together they are the whole fail-closed surface between a request and an identity: anything the session cannot supply exactly leaves the request anonymous rather than partly authenticated. */
func TestSessionTokenResolverFailsClosed(t *testing.T) {
    for name, request := range map[string]func(*testing.T) melodysecuritycontract.Token{
        "no request at all": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver(currentAccountLookup)(nil)
        },
        "no session published on the request": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver(currentAccountLookup)(requestAccepting(t, ""))
        },
        "something that is not a session under the session attribute": func(t *testing.T) melodysecuritycontract.Token {
            return SessionTokenResolver(currentAccountLookup)(requestCarryingSessionAttribute(t, "not a session"))
        },
        "a session with no user id": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER"})
            withCurrentCredential(sessionInstance)

            return SessionTokenResolver(currentAccountLookup)(request)
        },
        "a user id that is not a string": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, 7)
            sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER"})
            withCurrentCredential(sessionInstance)

            return SessionTokenResolver(currentAccountLookup)(request)
        },
        "an empty user id": func(t *testing.T) melodysecuritycontract.Token {
            return signedIn(t, "", []string{"ROLE_USER"})
        },
        "a session with no roles": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")
            withCurrentCredential(sessionInstance)

            return SessionTokenResolver(currentAccountLookup)(request)
        },
        "roles that are not a string slice": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")
            sessionInstance.Set(SessionKeySecurityRoles, "ROLE_USER")
            withCurrentCredential(sessionInstance)

            return SessionTokenResolver(currentAccountLookup)(request)
        },
        "an empty role list": func(t *testing.T) melodysecuritycontract.Token {
            return signedIn(t, "user-1", []string{})
        },
        "a restored role list carrying a non-string": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")
            sessionInstance.Set(SessionKeySecurityRoles, []any{"ROLE_USER", 7})
            withCurrentCredential(sessionInstance)

            return SessionTokenResolver(currentAccountLookup)(request)
        },
        "an empty restored role list": func(t *testing.T) melodysecuritycontract.Token {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-1")
            sessionInstance.Set(SessionKeySecurityRoles, []any{})
            withCurrentCredential(sessionInstance)

            return SessionTokenResolver(currentAccountLookup)(request)
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

/* the session's authority is the account's current one: every change to the account after the login is read on the next request, and a session the account does not back is emptied so it cannot come back once the account does */
func TestSessionTokenResolverAnswersTheAccountsCurrentAuthority(t *testing.T) {
    for name, scenario := range map[string]struct {
        sessionVersion any
        account        func(userId string) (*entity.User, bool, error)
        authenticated  bool
        roles          []string
        cleared        bool
    }{
        "an account demoted since the login answers its current role only": {
            sessionVersion: SessionCredentialVersion("original-hash"),
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "original-hash", []string{"ROLE_USER"}), true, nil
            },
            authenticated: true,
            roles:         []string{"ROLE_USER"},
        },
        "a deleted account": {
            sessionVersion: SessionCredentialVersion("original-hash"),
            account: func(userId string) (*entity.User, bool, error) {
                return nil, false, nil
            },
            cleared: true,
        },
        "a changed password": {
            sessionVersion: SessionCredentialVersion("original-hash"),
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "replacement-hash", []string{"ROLE_EDITOR"}), true, nil
            },
            cleared: true,
        },
        "an account left without roles": {
            sessionVersion: SessionCredentialVersion("original-hash"),
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "original-hash", nil), true, nil
            },
            cleared: true,
        },
        "an account without a password hash": {
            sessionVersion: SessionCredentialVersion(""),
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "", []string{"ROLE_EDITOR"}), true, nil
            },
            cleared: true,
        },
        "the lookup answering another account": {
            sessionVersion: SessionCredentialVersion("original-hash"),
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser("another-user", "editor", "original-hash", []string{"ROLE_EDITOR"}), true, nil
            },
            cleared: true,
        },
        "a session written before the credential version": {
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "original-hash", []string{"ROLE_EDITOR"}), true, nil
            },
            cleared: true,
        },
        "a credential version that is not a string": {
            sessionVersion: 7,
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "original-hash", []string{"ROLE_EDITOR"}), true, nil
            },
            cleared: true,
        },
        "an empty credential version": {
            sessionVersion: "",
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "", []string{"ROLE_EDITOR"}), true, nil
            },
            cleared: true,
        },
        "a session written before the credential version is cleared without asking for the account": {
            account: func(userId string) (*entity.User, bool, error) {
                return nil, false, errors.New("the account repository is unavailable")
            },
            cleared: true,
        },
        "an account the lookup did not find": {
            sessionVersion: SessionCredentialVersion("original-hash"),
            account: func(userId string) (*entity.User, bool, error) {
                return entity.NewUser(userId, "editor", "original-hash", []string{"ROLE_EDITOR"}), false, nil
            },
            cleared: true,
        },
        "a failing lookup leaves the session to the next request": {
            sessionVersion: SessionCredentialVersion("original-hash"),
            account: func(userId string) (*entity.User, bool, error) {
                return nil, false, errors.New("the account repository is unavailable")
            },
        },
    } {
        t.Run(name, func(t *testing.T) {
            request, sessionInstance := requestCarryingSession(t)
            sessionInstance.Set(SessionKeySecurityUserId, "user-2")
            sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_EDITOR"})
            if nil != scenario.sessionVersion {
                sessionInstance.Set(SessionKeySecurityCredentialVersion, scenario.sessionVersion)
            }

            lookup := func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
                return scenario.account(userId)
            }

            token := SessionTokenResolver(lookup)(request)

            if scenario.authenticated != token.IsAuthenticated() {
                t.Fatalf("expected authenticated %v, got %v with roles %v", scenario.authenticated, token.IsAuthenticated(), token.Roles())
            }

            if true == scenario.authenticated && (len(scenario.roles) != len(token.Roles()) || scenario.roles[0] != token.Roles()[0]) {
                t.Fatalf("expected the current roles %v, got %v", scenario.roles, token.Roles())
            }

            if scenario.cleared == sessionInstance.Has(SessionKeySecurityUserId) {
                t.Fatalf("expected the session cleared %v, it still carries the identity: %v", scenario.cleared, sessionInstance.Has(SessionKeySecurityUserId))
            }
        })
    }
}

/* resolverJournal keeps the error records the resolver writes; every other level falls to the embedded nop logger */
type resolverJournal struct {
    melodyloggingcontract.Logger
    messageList []string
    contextList []melodyloggingcontract.Context
}

func (instance *resolverJournal) Error(message string, context melodyloggingcontract.Context) {
    instance.messageList = append(instance.messageList, message)
    instance.contextList = append(instance.contextList, context)
}

/* requestCarryingSessionAndJournal is requestCarryingSession on a runtime whose logger is the journal handed back */
func requestCarryingSessionAndJournal(t *testing.T) (melodyhttpcontract.Request, melodysessioncontract.Session, *resolverJournal) {
    t.Helper()

    journal := &resolverJournal{Logger: melodylogging.NewNopLogger()}

    containerInstance := melodycontainer.NewContainer()
    melodycontainer.MustRegister[melodyloggingcontract.Logger](
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return journal, nil
        },
    )

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/products/", nil), nil, runtimeInstance, nil)

    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)
    sessionInstance := manager.NewSession()
    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    return request, sessionInstance, journal
}

func TestSessionTokenResolver_FilesAFailedLookupAndKeepsTheSession(t *testing.T) {
    request, sessionInstance, journal := requestCarryingSessionAndJournal(t)
    sessionInstance.Set(SessionKeySecurityUserId, "user-2")
    sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_EDITOR"})
    sessionInstance.Set(SessionKeySecurityCredentialVersion, SessionCredentialVersion("original-hash"))

    token := SessionTokenResolver(func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
        return nil, false, errors.New("the account repository is unavailable")
    })(request)

    if true == token.IsAuthenticated() || false == sessionInstance.Has(SessionKeySecurityUserId) {
        t.Fatalf("expected an anonymous answer over a kept session, got authenticated %v, kept %v", token.IsAuthenticated(), sessionInstance.Has(SessionKeySecurityUserId))
    }

    if 1 != len(journal.messageList) || "the account repository is unavailable" != journal.contextList[0]["error"] {
        t.Fatalf("expected the failed lookup filed once with its cause, got %v %v", journal.messageList, journal.contextList)
    }

    notFoundRequest, notFoundSession, notFoundJournal := requestCarryingSessionAndJournal(t)
    notFoundSession.Set(SessionKeySecurityUserId, "user-2")
    notFoundSession.Set(SessionKeySecurityRoles, []string{"ROLE_EDITOR"})
    notFoundSession.Set(SessionKeySecurityCredentialVersion, SessionCredentialVersion("original-hash"))

    _ = SessionTokenResolver(func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
        return nil, false, nil
    })(notFoundRequest)

    if 0 != len(notFoundJournal.messageList) || true == notFoundSession.Has(SessionKeySecurityUserId) {
        t.Fatalf("expected an account not found cleared without a record, got %v, kept %v", notFoundJournal.messageList, notFoundSession.Has(SessionKeySecurityUserId))
    }
}
