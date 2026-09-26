package security

import (
    "strings"

    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewPathPrefixMatcher(prefix string) *PathPrefixMatcher {
    return &PathPrefixMatcher{
        prefix: prefix,
    }
}

type PathPrefixMatcher struct {
    prefix string
}

func (instance *PathPrefixMatcher) Matches(request httpcontract.Request) bool {
    /* IsNilInterface: the request is an application-implementable contract, and the next line dereferences it */
    if true == internal.IsNilInterface(request) {
        return false
    }

    if nil == request.HttpRequest() {
        return false
    }

    if nil == request.HttpRequest().URL {
        return false
    }

    path := request.HttpRequest().URL.Path

    if "" == instance.prefix {
        return true
    }

    return true == strings.HasPrefix(path, instance.prefix)
}

var _ securitycontract.Matcher = (*PathPrefixMatcher)(nil)
