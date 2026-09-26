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

    /* the spelling the router reads, so a firewall written for "/admin/" does not claim "/admin%2Fusers", a one-segment resource the router never routes under "/admin"; the access-control matcher reads the same spelling */
    path := http.RequestPathAsRouted(internal.RequestPathAsSent(request.HttpRequest().URL))

    if "" == instance.prefix {
        return true
    }

    if true == strings.HasPrefix(path, instance.prefix) {
        return true
    }

    /* the router reads "/admin/" and "/admin" as the same route, so a prefix written with the trailing slash also claims the bare spelling, and only it: "/admin/" still selects nothing under "/administrator" */
    trimmedPrefix := strings.TrimRight(instance.prefix, "/")
    if "" != trimmedPrefix && path == trimmedPrefix {
        return true
    }

    return false
}

var _ securitycontract.Matcher = (*PathPrefixMatcher)(nil)
