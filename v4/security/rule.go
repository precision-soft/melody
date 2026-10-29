package security

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* ApiKeyHeaderRuleConfig names the header the rule checks and the value it must carry; both are required. */
type ApiKeyHeaderRuleConfig struct {
    HeaderName    string
    ExpectedValue string
}

/* NewApiKeyHeaderRule checks the header on the requests the matcher claims; the matcher is required, as are both fields, each refused by name. */
func NewApiKeyHeaderRule(matcher securitycontract.Matcher, config ApiKeyHeaderRuleConfig) *ApiKeyHeaderRule {
    if true == internal.IsNilInterface(matcher) {
        exception.Panic(exception.NewError("api key header rule matcher is nil", nil, nil))
    }

    if "" == config.HeaderName {
        exception.Panic(exception.NewError("api key header rule header name is empty", exceptioncontract.Context{"field": "ApiKeyHeaderRuleConfig.HeaderName"}, nil))
    }

    if "" == config.ExpectedValue {
        exception.Panic(exception.NewError("api key header rule expected value is empty", exceptioncontract.Context{"field": "ApiKeyHeaderRuleConfig.ExpectedValue"}, nil))
    }

    return &ApiKeyHeaderRule{
        matcher:       matcher,
        headerName:    config.HeaderName,
        expectedValue: config.ExpectedValue,
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

    /* IsNilInterface: a custom matcher may claim a request the framework's matcher would refuse, and the header read below dereferences it */
    if true == internal.IsNilInterface(request) {
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
