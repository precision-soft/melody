package security

import (
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
    melodysessioncontract "github.com/precision-soft/melody/session/contract"
)

const (
    SessionKeySecurityUserId = "security.userId"
    SessionKeySecurityRoles  = "security.roles"
)

func SessionTokenResolver() melodysecuritycontract.TokenResolver {
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

        return melodysecurity.NewAuthenticatedToken(
            userId,
            roles,
        )
    }
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

/* getStringSliceFromSession accepts the two spellings a role list has in a session: the []string the login handler writes, and the []any a file-backed storage answers after a restart — its snapshot round-trips through json, which keeps no element type. The second form is accepted only when EVERY element is a string; anything else stays a refusal. */
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
