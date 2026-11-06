package security

import (
    stdpath "path"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewPathPrefixMatcher selects a firewall for every request path that begins with prefix, compared as a string prefix: "/api" claims "/api-admin" and "/apiary" as well as "/api/users", and a prefix ending in a slash also claims its bare spelling, "/admin/" selecting "/admin" and nothing under "/administrator". NewSegmentPrefixMatcher is the reach a firewall over a tree usually means. The first firewall whose matcher matches wins, so a firewall declared after one whose prefix begins its own is never selected and its access control never runs: declare the narrower firewall first, or end the earlier prefix at a segment boundary ("/api/"); such a later firewall is named in a boot warning. An empty prefix claims every path. The path is written as the router reads it, decoded, with %2F for a separator inside a segment: a percent escape other than %2F claims only a resource whose name holds the percent sign literally, and is named in a boot warning. The prefix is read as written, never trimmed. A prefix no request path the kernel admits can carry, one that does not begin with a slash or carries an empty, "." or ".." segment, is refused: the kernel matches only a canonical request path, so the firewall would never be selected. */
func NewPathPrefixMatcher(prefix string) *PathPrefixMatcher {
    if true == firewallPrefixIsUnreachable(prefix) {
        exception.Panic(
            exception.NewError(
                "firewall path prefix is never selected: it does not begin with a slash or carries an empty, . or .. segment, and the kernel matches only a canonical request path; write it canonical, beginning with a slash",
                exceptioncontract.Context{
                    "field":  "NewPathPrefixMatcher.prefix",
                    "prefix": prefix,
                },
                nil,
            ),
        )
    }

    return &PathPrefixMatcher{
        prefix: prefix,
    }
}

type PathPrefixMatcher struct {
    prefix string
}

func (instance *PathPrefixMatcher) Matches(request httpcontract.Request) bool {
    path, readable := matchedRequestPath(request)
    if false == readable {
        return false
    }

    return instance.ClaimsPath(path)
}

/* ClaimsPath reports whether the matcher selects this path as the router spells it, which is what a firewall door's reach is checked against at boot. */
func (instance *PathPrefixMatcher) ClaimsPath(path string) bool {
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

/* Prefix is the raw prefix the matcher compares the request path with, read as a string prefix: "/api" claims "/api-admin" too, and "/admin/" claims the bare "/admin" */
func (instance *PathPrefixMatcher) Prefix() string {
    return instance.prefix
}

/* String describes the matcher for the compiled firewall and the security context, as path prefix "/admin". */
func (instance *PathPrefixMatcher) String() string {
    return "path prefix " + strconv.Quote(instance.prefix)
}

var _ securitycontract.Matcher = (*PathPrefixMatcher)(nil)

/* NewSegmentPrefixMatcher selects a firewall for the request path prefix and every path beneath it under a "/" boundary: "/api" claims "/api" and "/api/users" but not "/api-admin" or "/apiary", the reach of accesscontrol.NewSegmentPrefixRule. Surrounding whitespace is trimmed and one trailing slash is folded away, so "/api/" and "/api" declare the same matcher, and "/" claims every path. An empty prefix, one that does not begin with a slash, and one that carries an empty, "." or ".." segment are refused naming the argument. The path is written as the router reads it, decoded, with %2F for a separator inside a segment: a percent escape other than %2F claims only a resource whose name holds the percent sign literally, and is named in a boot warning. */
func NewSegmentPrefixMatcher(prefix string) *SegmentPrefixMatcher {
    declaredPrefix := strings.TrimSpace(prefix)
    if "" == declaredPrefix {
        refuseSegmentPrefix("firewall segment prefix may not be empty; declare \"/\" to claim every path", prefix)
    }

    if false == strings.HasPrefix(declaredPrefix, "/") {
        refuseSegmentPrefix("firewall segment prefix must begin with a slash", prefix)
    }

    if "/" != declaredPrefix {
        trimmedPrefix := strings.TrimSuffix(declaredPrefix, "/")
        if true == strings.HasSuffix(trimmedPrefix, "/") || stdpath.Clean(trimmedPrefix) != trimmedPrefix {
            refuseSegmentPrefix("firewall segment prefix must be canonical: no empty, . or .. segment", prefix)
        }

        declaredPrefix = trimmedPrefix
    }

    return &SegmentPrefixMatcher{
        prefix: declaredPrefix,
    }
}

type SegmentPrefixMatcher struct {
    prefix string
}

func (instance *SegmentPrefixMatcher) Matches(request httpcontract.Request) bool {
    path, readable := matchedRequestPath(request)
    if false == readable {
        return false
    }

    return instance.ClaimsPath(path)
}

/* ClaimsPath reports whether the matcher selects this path as the router spells it, which is what a firewall door's reach is checked against at boot. */
func (instance *SegmentPrefixMatcher) ClaimsPath(path string) bool {
    return true == segmentPrefixClaims(instance.prefix, path)
}

/* Prefix is the folded prefix the matcher claims under a segment boundary: "/api" for a matcher declared as "/api/", "/" for the catch-all */
func (instance *SegmentPrefixMatcher) Prefix() string {
    return instance.prefix
}

/* String describes the matcher for the compiled firewall and the security context, as segment prefix "/admin". */
func (instance *SegmentPrefixMatcher) String() string {
    return "segment prefix " + strconv.Quote(instance.prefix)
}

var _ securitycontract.Matcher = (*SegmentPrefixMatcher)(nil)

func segmentPrefixClaims(prefix string, path string) bool {
    if "/" == prefix {
        return true
    }

    return path == prefix || true == strings.HasPrefix(path, prefix+"/")
}

func refuseSegmentPrefix(message string, prefix string) {
    exception.Panic(
        exception.NewError(
            message,
            exceptioncontract.Context{
                "field":  "NewSegmentPrefixMatcher.prefix",
                "prefix": prefix,
            },
            nil,
        ),
    )
}

/* matchedRequestPath reads the routed spelling, "/" prepended to a target without one, as the router and the access-control matcher do */
func matchedRequestPath(request httpcontract.Request) (string, bool) {
    /* IsNilInterface: the request is an application-implementable contract, and the next line dereferences it */
    if true == internal.IsNilInterface(request) {
        return "", false
    }

    if nil == request.HttpRequest() || nil == request.HttpRequest().URL {
        return "", false
    }

    path := http.RequestPathAsRouted(internal.RequestPathAsSent(request.HttpRequest().URL))
    if false == strings.HasPrefix(path, "/") {
        path = "/" + path
    }

    return path, true
}

/* firewallPrefixIsUnreachable reports a raw prefix no canonical request path can begin with. "" and "/" claim every path. The matcher also selects the bare spelling of a prefix written with one trailing slash, so it is dropped; a prefix without one is a string prefix, so its last segment may still grow into a canonical one ("/api/." claims "/api/.well-known") and is judged extended. */
func firewallPrefixIsUnreachable(prefix string) bool {
    if "" == prefix || "/" == prefix {
        return false
    }

    if false == strings.HasPrefix(prefix, "/") {
        return true
    }

    /* more than one trailing slash is an empty segment no canonical path carries, and the fold would leave the prefix selecting its bare spelling alone */
    if true == strings.HasSuffix(prefix, "//") {
        return true
    }

    declaredPrefix := strings.TrimRight(prefix, "/")
    if "" == declaredPrefix {
        return false
    }

    if false == strings.HasSuffix(prefix, "/") {
        declaredPrefix = declaredPrefix + "x"
    }

    return stdpath.Clean(declaredPrefix) != declaredPrefix
}
