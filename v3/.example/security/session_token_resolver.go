package security

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

const (
    SessionKeySecurityUserId            = "security.userId"
    SessionKeySecurityRoles             = "security.roles"
    SessionKeySecurityCredentialVersion = "security.credentialVersion"
)

/* SessionTokenResolver answers the token of the account the session names, with the account's CURRENT roles. A session that carries no credential version, or one the account does not hold, or that names an account which is gone or holds no role, is cleared and answers anonymous; a lookup that fails answers anonymous for this request, leaves the session alone, since the failure says nothing about the account, and files the cause at error. */
func SessionTokenResolver(lookupUser SessionUserLookup) melodysecuritycontract.TokenResolver {
    return func(request melodyhttpcontract.Request) melodysecuritycontract.Token {
        sessionInstance := getSession(request)
        if nil == sessionInstance {
            return melodysecurity.NewAnonymousToken()
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
            examplejournal.LoggerOr(request.RuntimeInstance(), melodylogging.EmergencyLogger()).Error("session account lookup failed; the request is served anonymous and the session is kept", melodyexception.LogContext(lookupErr))

            return melodysecurity.NewAnonymousToken()
        }

        if false == sessionAccountIsCurrent(user, found, userId, credentialVersion) {
            sessionInstance.Clear()

            return melodysecurity.NewAnonymousToken()
        }

        return melodysecurity.NewAuthenticatedToken(
            user.Id,
            user.Roles,
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

func getSession(request melodyhttpcontract.Request) melodysessioncontract.Session {
    if nil == request {
        return nil
    }

    attributes := request.Attributes()
    if nil == attributes {
        return nil
    }

    value, exists := attributes.Get(melodyhttp.RequestAttributeSession)
    if false == exists {
        return nil
    }

    sessionInstance, ok := value.(melodysessioncontract.Session)
    if false == ok {
        return nil
    }

    return sessionInstance
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
