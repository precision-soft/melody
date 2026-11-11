package security

import (
    "github.com/precision-soft/melody/v4/.example/entity"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

const (
    SessionKeySecurityUserId            = "security.userId"
    SessionKeySecurityRoles             = "security.roles"
    SessionKeySecurityCredentialVersion = "security.credentialVersion"
)

/* SessionTokenResolver answers the token of the account the session names, with the account's CURRENT roles. A session its storage could not load, or the empty one served for it under MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE, answers the unavailable token; any other error reading the session answers anonymous, its record filed by the read. A session that carries no credential version, or one the account does not hold, or that names an account which is gone or holds no role, is cleared and answers anonymous; a lookup that fails answers the unavailable token, 503 with Retry-After on a protected page, and leaves the session alone, since the failure says nothing about the account; the framework files the cause at error. */
func SessionTokenResolver(lookupUser SessionUserLookup) melodysecuritycontract.TokenResolver {
    return func(request melodyhttpcontract.Request) melodysecuritycontract.Token {
        sessionInstance, sessionErr := melodyhttp.RequestSession(request)
        if nil != sessionErr {
            /* a session its storage could not load says nothing about the visitor: a public page is served as not signed in, a protected one answers 503 with Retry-After */
            if true == melodyexception.AnswersUnavailable(sessionErr) {
                return melodysecurity.NewUnavailableToken(sessionErr)
            }

            return melodysecurity.NewAnonymousToken()
        }

        /* the empty session MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE serves in an outage reads as the outage it stands for, so a signed-in visitor is answered 503 on a protected page, not sent to sign in */
        if cause, isUnavailable := melodysession.SessionUnavailableCause(sessionInstance); true == isUnavailable {
            return melodysecurity.NewUnavailableToken(cause)
        }

        userId, ok := getStringFromSession(sessionInstance, SessionKeySecurityUserId)
        if false == ok {
            return melodysecurity.NewAnonymousToken()
        }

        if "" == userId {
            return melodysecurity.NewAnonymousToken()
        }

        roles, ok := getStringSliceFromSession(sessionInstance, SessionKeySecurityRoles)
        if false == ok {
            return melodysecurity.NewAnonymousToken()
        }

        if 0 == len(roles) {
            return melodysecurity.NewAnonymousToken()
        }

        credentialVersion, ok := getStringFromSession(sessionInstance, SessionKeySecurityCredentialVersion)
        if false == ok || "" == credentialVersion {
            sessionInstance.Clear()

            return melodysecurity.NewAnonymousToken()
        }

        user, found, lookupErr := lookupUser(request, userId)
        if nil != lookupErr {
            return melodysecurity.NewUnavailableToken(
                melodyexception.NewError(
                    "the account a session names could not be read",
                    melodyexceptioncontract.Context{"userId": userId},
                    melodyexception.MarkInfrastructureFailure(lookupErr),
                ),
            )
        }

        if false == sessionAccountIsCurrent(user, found, userId, credentialVersion) {
            sessionInstance.Clear()

            return melodysecurity.NewAnonymousToken()
        }

        return melodysecurity.NewAuthenticatedToken(
            user.Id,
            user.Roles,
            nil,
        )
    }
}

func sessionAccountIsCurrent(user *entity.User, found bool, userId string, credentialVersion string) bool {
    if false == found || nil == user {
        return false
    }

    if userId != user.Id || "" == user.Password || 0 == len(user.Roles) {
        return false
    }

    return credentialVersion == SessionCredentialVersion(user.Password)
}

func getStringFromSession(sessionInstance melodysessioncontract.Session, key string) (string, bool) {
    if false == sessionInstance.Has(key) {
        return "", false
    }

    value := sessionInstance.Get(key)

    typed, ok := value.(string)
    if false == ok {
        return "", false
    }

    return typed, true
}

/* getStringSliceFromSession accepts the two spellings a role list has in a session: the []string the login handler writes, and the []any a file-backed storage answers after a restart, since its json snapshot keeps no element type. The second form is accepted only when every element is a string. */
func getStringSliceFromSession(sessionInstance melodysessioncontract.Session, key string) ([]string, bool) {
    if false == sessionInstance.Has(key) {
        return nil, false
    }

    value := sessionInstance.Get(key)

    typed, ok := value.([]string)
    if true == ok {
        return typed, true
    }

    untyped, ok := value.([]any)
    if false == ok {
        return nil, false
    }

    restored := make([]string, 0, len(untyped))
    for _, element := range untyped {
        elementString, elementOk := element.(string)
        if false == elementOk {
            return nil, false
        }

        restored = append(restored, elementString)
    }

    return restored, true
}
