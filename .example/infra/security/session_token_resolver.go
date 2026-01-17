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

func getStringSliceFromSession(sessionInstance melodysessioncontract.Session, key string) ([]string, bool) {
	if false == sessionInstance.Has(key) {
		return nil, false
	}

	value := sessionInstance.Get(key)

	typed, ok := value.([]string)
	if false == ok {
		return nil, false
	}

	return typed, true
}
