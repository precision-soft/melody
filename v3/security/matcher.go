package security

import (
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
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
    if nil == request {
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
