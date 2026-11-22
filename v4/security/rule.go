package security

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* ApiKeyHeaderRuleConfig names the header the rule checks, DefaultApiKeyHeaderName when empty. */
type ApiKeyHeaderRuleConfig struct {
    HeaderName string
}

/* NewApiKeyHeaderRule checks that the header carries expectedValue on the requests the matcher claims; a nil matcher or an empty expectedValue panics naming the argument. */
func NewApiKeyHeaderRule(matcher securitycontract.Matcher, expectedValue string, config ApiKeyHeaderRuleConfig) *ApiKeyHeaderRule {
    if true == internal.IsNilInterface(matcher) {
        exception.Panic(exception.NewError("api key header rule matcher is nil", exceptioncontract.Context{"argument": "matcher"}, nil))
    }

    if "" == expectedValue {
        exception.Panic(exception.NewError("api key header rule expected value is empty", exceptioncontract.Context{"argument": "expectedValue"}, nil))
    }

    headerName := config.HeaderName
    if "" == headerName {
        headerName = DefaultApiKeyHeaderName
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

func (instance *ApiKeyHeaderRule) Check(runtimeInstance containercontract.Runtime, request httpcontract.Request) error {
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
