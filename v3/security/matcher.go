package security

import (
    "strings"

    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

    /* the spelling the router reads, so a firewall written for "/admin/" does not claim "/admin%2Fusers", a one-segment resource the router never routes under "/admin" — the access-control matcher behind the firewall reads the same spelling */
    path := http.RequestPathAsRouted(request.HttpRequest().URL.EscapedPath())

    if "" == instance.prefix {
        return true
    }

    if true == strings.HasPrefix(path, instance.prefix) {
        return true
    }

    /* the router reads "/admin/" and "/admin" as the same route, so a prefix written with the trailing slash also claims the bare spelling — without this, the unwritten spelling escaped the firewall that named the other, and the route was reachable both guarded and unguarded. Only the exact bare spelling is added: the comparison stays the plain prefix test otherwise, so "/admin/" still selects nothing under "/administrator", and the static excluded paths mirror the same reading. */
    trimmedPrefix := strings.TrimRight(instance.prefix, "/")
    if "" != trimmedPrefix && path == trimmedPrefix {
        return true
    }

    return false
}

var _ securitycontract.Matcher = (*PathPrefixMatcher)(nil)
