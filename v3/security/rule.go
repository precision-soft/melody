package security

import (
    "crypto/subtle"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewApiKeyHeaderRule(matcher securitycontract.Matcher, headerName string, expectedValue string) *ApiKeyHeaderRule {
    if "" == headerName {
        exception.Panic(exception.NewError("api key header rule header name is empty", nil, nil))
    }

    if "" == expectedValue {
        exception.Panic(exception.NewError("api key header rule expected value is empty", nil, nil))
    }

    return &ApiKeyHeaderRule{
        matcher:       matcher,
        headerName:    headerName,
        expectedValue: expectedValue,
    }
}

type ApiKeyHeaderRule struct {
    matcher       securitycontract.Matcher
    headerName    string
    expectedValue string
}

func (instance *ApiKeyHeaderRule) Applies(request httpcontract.Request) bool {
    return instance.matcher.Matches(request)
}

func (instance *ApiKeyHeaderRule) Check(request httpcontract.Request) error {
    if false == instance.Applies(request) {
        return nil
    }

    if nil == request {
        return exception.Forbidden("forbidden")
    }

    if nil == request.HttpRequest() {
        return exception.Forbidden("forbidden")
    }

    headerValue := request.HttpRequest().Header.Get(instance.headerName)

    expectedBytes := []byte(instance.expectedValue)
    headerBytes := []byte(headerValue)

    if 1 == subtle.ConstantTimeCompare(expectedBytes, headerBytes) {
        return nil
    }

    return exception.Forbidden("forbidden")
}

var _ securitycontract.Rule = (*ApiKeyHeaderRule)(nil)
