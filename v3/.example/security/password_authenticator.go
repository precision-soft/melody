package security

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* the request attributes the sign-in door hands its authenticators: the credentials it parsed from its body, and the account the password authenticator accepted */
const (
    RequestAttributeLoginUsername = "example.login.username"
    RequestAttributeLoginPassword = "example.login.password"
    RequestAttributeLoginAccount  = "example.login.account"
)

/* PasswordCheck answers the account a username and password name, and false for a pair it refuses; an error is a failure to decide, never a refusal. */
type PasswordCheck func(runtimeInstance melodyruntimecontract.Runtime, username string, password string) (*entity.User, bool, error)

/* NewPasswordAuthenticator is the first factor of the sign-in. It reads the credentials the door set as request attributes, never the body the door already consumed, and publishes the account it accepted on the request, so the door writes the session without reading the account a second time. */
func NewPasswordAuthenticator(check PasswordCheck) *PasswordAuthenticator {
    return &PasswordAuthenticator{check: check}
}

type PasswordAuthenticator struct {
    check PasswordCheck
}

func (instance *PasswordAuthenticator) Supports(request melodyhttpcontract.Request) bool {
    username, password := loginCredentials(request)

    return "" != username && "" != password
}

func (instance *PasswordAuthenticator) Authenticate(request melodyhttpcontract.Request) (melodysecuritycontract.Token, error) {
    username, password := loginCredentials(request)

    account, accepted, checkErr := instance.check(request.RuntimeInstance(), username, password)
    if nil != checkErr {
        return nil, checkErr
    }

    if false == accepted || nil == account {
        return melodysecurity.NewAnonymousToken(), nil
    }

    request.Attributes().Set(RequestAttributeLoginAccount, account)

    return melodysecurity.NewAuthenticatedToken(account.Id, append([]string{}, account.Roles...)), nil
}

/* LoginAccount answers the account the password authenticator accepted on this request. */
func LoginAccount(request melodyhttpcontract.Request) (*entity.User, bool) {
    value, exists := request.Attributes().Get(RequestAttributeLoginAccount)
    if false == exists {
        return nil, false
    }

    account, isAccount := value.(*entity.User)
    if false == isAccount || nil == account {
        return nil, false
    }

    return account, true
}

func loginCredentials(request melodyhttpcontract.Request) (string, string) {
    attributes := request.Attributes()
    if nil == attributes {
        return "", ""
    }

    return attributeString(attributes.Get(RequestAttributeLoginUsername)), attributeString(attributes.Get(RequestAttributeLoginPassword))
}

func attributeString(value any, exists bool) string {
    if false == exists {
        return ""
    }

    text, isText := value.(string)
    if false == isText {
        return ""
    }

    return text
}

var _ melodysecuritycontract.Authenticator = (*PasswordAuthenticator)(nil)
