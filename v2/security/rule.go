package security

import (
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewApiKeyHeaderRule(matcher securitycontract.Matcher, headerName string, expectedValue string) *ApiKeyHeaderRule {
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
    if instance.expectedValue == headerValue {
        return nil
    }

    return exception.Forbidden("forbidden")
}

var _ securitycontract.Rule = (*ApiKeyHeaderRule)(nil)
