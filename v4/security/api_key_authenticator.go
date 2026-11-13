package security

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* ApiKeyHeaderAuthenticatorConfig names the header, the secret it must carry and the principal a match authenticates as; HeaderName and ExpectedValue are required. */
type ApiKeyHeaderAuthenticatorConfig struct {
    HeaderName     string
    ExpectedValue  string
    UserIdentifier string
    Roles          []string
}

/* NewApiKeyHeaderAuthenticator builds the authenticator, panicking on an empty HeaderName or ExpectedValue by the field's name; the roles are copied. */
func NewApiKeyHeaderAuthenticator(config ApiKeyHeaderAuthenticatorConfig) *ApiKeyHeaderAuthenticator {
    if "" == config.HeaderName {
        exception.Panic(
            exception.NewError("the header name is empty in api key header authenticator", exceptioncontract.Context{"field": "ApiKeyHeaderAuthenticatorConfig.HeaderName"}, nil),
        )
    }

    if "" == config.ExpectedValue {
        exception.Panic(
            exception.NewError("the expected value is empty in api key header authenticator", exceptioncontract.Context{"field": "ApiKeyHeaderAuthenticatorConfig.ExpectedValue"}, nil),
        )
    }

    return &ApiKeyHeaderAuthenticator{
        headerName:    config.HeaderName,
        expectedValue: config.ExpectedValue,
        userId:        config.UserIdentifier,
        roles:         append([]string{}, config.Roles...),
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

    if false == constantTimeSecretEquals(instance.expectedValue, headerValue) {
        return NewAnonymousToken(), nil
    }

    return NewAuthenticatedToken(instance.userId, instance.roles, nil), nil
}

var _ securitycontract.Authenticator = (*ApiKeyHeaderAuthenticator)(nil)
