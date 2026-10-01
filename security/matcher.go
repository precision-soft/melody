package security

import (
    "strings"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    securitycontract "github.com/precision-soft/melody/security/contract"
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

    /* a request target that does not begin with "/", the empty path of an absolute-form or authority-form target or the asterisk-form "*", is read with the "/" prepended, the way the router and the access-control matcher read it, so the three agree on one spelling and the firewall that owns "/" is selected for it rather than skipped */
    if false == strings.HasPrefix(path, "/") {
        path = "/" + path
    }

    if "" == instance.prefix {
        return true
    }

    return true == strings.HasPrefix(path, instance.prefix)
}

var _ securitycontract.Matcher = (*PathPrefixMatcher)(nil)
