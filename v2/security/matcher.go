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
    /* IsNilInterface and not `nil ==`: Matches is public and the request is an application-implementable
    contract, so a nil pointer of a request type arrives as a non-nil interface a bare check reads as a live
    request — and the very next line dereferences it, taking the request down on the path that decides
    which firewall claims it. */
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
