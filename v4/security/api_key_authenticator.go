package security

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* DefaultApiKeyHeaderName is the header an api key authenticator or rule reads when its HeaderName is left empty. */
const DefaultApiKeyHeaderName = "X-Api-Key"

/* ApiKeyHeaderAuthenticatorConfig names the header (DefaultApiKeyHeaderName when empty) and the principal a match authenticates as. */
type ApiKeyHeaderAuthenticatorConfig struct {
    HeaderName     string
    UserIdentifier string
    Roles          []string
}

/* NewApiKeyHeaderAuthenticator authenticates a request whose header carries expectedValue, panicking on an empty expectedValue naming the argument; the roles are copied. */
func NewApiKeyHeaderAuthenticator(expectedValue string, config ApiKeyHeaderAuthenticatorConfig) *ApiKeyHeaderAuthenticator {
    if "" == expectedValue {
        exception.Panic(
            exception.NewError("the expected value is empty in api key header authenticator", exceptioncontract.Context{"argument": "expectedValue"}, nil),
        )
    }

    headerName := config.HeaderName
    if "" == headerName {
        headerName = DefaultApiKeyHeaderName
    }

    return &ApiKeyHeaderAuthenticator{
        headerName:    headerName,
        expectedValue: expectedValue,
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

func (instance *ApiKeyHeaderAuthenticator) Authenticate(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    headerValue := request.Header(instance.headerName)

    if false == constantTimeSecretEquals(instance.expectedValue, headerValue) {
        return NewAnonymousToken(), nil
    }

    return NewAuthenticatedToken(instance.userId, instance.roles, nil), nil
}

var _ securitycontract.Authenticator = (*ApiKeyHeaderAuthenticator)(nil)
