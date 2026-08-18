package security

import (
    "strings"

    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
)

/* ApiKeyHeaderName is the header the api firewall and its authenticator both read; one constant, so the matcher cannot claim a request the authenticator then ignores. */
const ApiKeyHeaderName = "X-Api-Key"

/* NewApiKeyRequestMatcher claims a request only when the path carries the prefix AND the request presents the header. The header condition is what lets the api firewall coexist with the session firewall on the same paths: a browser sending its cookie carries no api key and falls through to the firewall behind this one, while a client that does present the key — right or wrong — is answered by the api firewall alone. */
func NewApiKeyRequestMatcher(pathPrefix string, headerName string) *ApiKeyRequestMatcher {
    return &ApiKeyRequestMatcher{
        pathPrefix: pathPrefix,
        headerName: headerName,
    }
}

type ApiKeyRequestMatcher struct {
    pathPrefix string
    headerName string
}

func (instance *ApiKeyRequestMatcher) Matches(request melodyhttpcontract.Request) bool {
    if nil == request {
        return false
    }

    if nil == request.HttpRequest() {
        return false
    }

    if nil == request.HttpRequest().URL {
        return false
    }

    if false == strings.HasPrefix(request.HttpRequest().URL.Path, instance.pathPrefix) {
        return false
    }

    return "" != request.Header(instance.headerName)
}

var _ melodysecuritycontract.Matcher = (*ApiKeyRequestMatcher)(nil)
