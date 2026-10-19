package security

import (
    "strings"

    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

/* NewPathPrefixMatcher selects a firewall for every request path that begins with prefix, compared as a string prefix: "/api" claims "/api-admin" and "/apiary" as well as "/api/users". The first firewall whose matcher matches wins, so a firewall declared after one whose prefix begins its own is never selected and its access control never runs: declare the narrower firewall first, or end the earlier prefix at a segment boundary ("/api/"). An empty prefix claims every path. The path is written as the router reads it, decoded. A request carrying an encoded separator (%2F) is refused by the kernel before any firewall, so a declared path never holds one; a percent escape written in a declaration claims only a resource whose name holds the percent sign literally. */
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
