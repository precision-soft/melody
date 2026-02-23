package security

import (
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewApiKeyHeaderAuthenticator(headerName string, expectedValue string, userId string, roles []string) *ApiKeyHeaderAuthenticator {
    if "" == headerName {
        exception.Panic(
            exception.NewError("the header name is empty in api key header authenticator", nil, nil),
        )
    }

    return &ApiKeyHeaderAuthenticator{
        headerName:    headerName,
        expectedValue: expectedValue,
        userId:        userId,
        roles:         append([]string{}, roles...),
    }
}

type ApiKeyHeaderAuthenticator struct {
    headerName    string
    expectedValue string
    userId        string
    roles         []string
}

func (instance *ApiKeyHeaderAuthenticator) Supports(request httpcontract.Request) bool {
    headerValue := request.Header(instance.headerName)

    return "" != headerValue
}

func (instance *ApiKeyHeaderAuthenticator) Authenticate(request httpcontract.Request) (securitycontract.Token, error) {
    headerValue := request.Header(instance.headerName)
    if instance.expectedValue != headerValue {
        return NewAnonymousToken(), nil
    }

    return NewAuthenticatedToken(instance.userId, instance.roles), nil
}

var _ securitycontract.Authenticator = (*ApiKeyHeaderAuthenticator)(nil)
