package security

import (
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewApiKeyHeaderRule(matcher securitycontract.Matcher, headerName string, expectedValue string) *ApiKeyHeaderRule {
    if true == internal.IsNilInterface(matcher) {
        exception.Panic(exception.NewError("api key header rule matcher is nil", nil, nil))
    }

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

    if true == constantTimeSecretEquals(instance.expectedValue, headerValue) {
        return nil
    }

    return exception.Forbidden("forbidden")
}

var _ securitycontract.Rule = (*ApiKeyHeaderRule)(nil)
