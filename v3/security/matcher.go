package security

import (
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* NewPathPrefixMatcher selects a firewall for every request path that begins with prefix, compared as a string prefix: "/api" claims "/api-admin" and "/apiary" as well as "/api/users". The first firewall whose matcher matches wins, so a firewall declared after one whose prefix begins its own is never selected and its access control never runs: declare the narrower firewall first, or end the earlier prefix at a segment boundary ("/api/"). An empty prefix claims every path. The path is written as the router reads it, decoded, with %2F for a separator inside a segment: a percent escape other than %2F claims only a resource whose name holds the percent sign literally. Such a prefix, and a later path-prefix firewall an earlier one shadows, is named in a boot warning. */
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

    /* a request target that does not begin with "/", the empty path of an absolute-form or authority-form target or the asterisk-form "*", is read with the "/" prepended, the way the router and the access-control matcher read it, so the three agree on one spelling and the firewall that owns "/" is selected for it rather than skipped */
    if false == strings.HasPrefix(path, "/") {
        path = "/" + path
    }

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

/* Prefix is the raw prefix the matcher compares the request path with, read as a string prefix: "/api" claims "/api-admin" too */
func (instance *PathPrefixMatcher) Prefix() string {
    return instance.prefix
}

/* String describes the matcher for the compiled firewall and the security context, as path prefix "/admin". */
func (instance *PathPrefixMatcher) String() string {
    return "path prefix " + strconv.Quote(instance.prefix)
}

var _ securitycontract.Matcher = (*PathPrefixMatcher)(nil)
