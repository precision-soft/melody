package cors

import (
    nethttp "net/http"
    "net/url"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
)

type Service struct {
    allowOrigins     []string
    allowMethods     []string
    allowHeaders     []string
    exposeHeaders    []string
    allowCredentials bool
    maxAge           int
    allowOriginFunc  func(origin string) bool

    allowMethodsString  string
    allowHeadersString  string
    exposeHeadersString string
    maxAgeString        string
}

type Config struct {
    AllowOrigins     []string
    AllowMethods     []string
    AllowHeaders     []string
    ExposeHeaders    []string
    AllowCredentials bool
    MaxAge           int
    AllowOriginFunc  func(origin string) bool
}

func NewService(config Config) *Service {
    allowOrigins := copyStrings(config.AllowOrigins)
    allowMethods := copyStrings(config.AllowMethods)
    allowHeaders := copyStrings(config.AllowHeaders)
    exposeHeaders := copyStrings(config.ExposeHeaders)

    /* a nil list expresses no preference and receives the permissive default; an EMPTY list is an expressed preference — no origin is allowed — and rewriting it to the wildcard would turn "nobody" into "everybody" the moment a misconfigured environment variable arrives empty. An empty list therefore denies every origin. */
    if nil == allowOrigins {
        allowOrigins = []string{"*"}
    }

    /* methods and headers read nil and empty the way origins do — nil takes the default, an empty list stays the expressed preference — because one Config must not read the same spelling as "nobody" on one field and "everything" on its sibling. The defaults are the single lists DefaultService grants, Authorization included: two default header lists meant an SPA sending Authorization worked on the zero-configuration deployment and died at preflight the moment the operator narrowed only the origins. */
    if nil == allowMethods {
        allowMethods = defaultAllowMethodList()
    }

    if nil == allowHeaders {
        allowHeaders = defaultAllowHeaderList()
    }

    if true == config.AllowCredentials && nil == config.AllowOriginFunc {
        /* credentials with nothing to grant them to is a contradiction worth stopping at boot: a deny-all service that also promises credentials can only come from a list that failed to load */
        if 0 == len(allowOrigins) {
            exception.Panic(
                exception.NewError(
                    "cors misconfiguration: allowCredentials cannot be true when no origin is allowed",
                    nil,
                    nil,
                ),
            )
        }

        for _, origin := range allowOrigins {
            if "*" == strings.TrimSpace(origin) {
                exception.Panic(
                    exception.NewError(
                        "cors misconfiguration: allowCredentials cannot be true when allowOrigins contains wildcard '*'",
                        nil,
                        nil,
                    ),
                )
            }
        }
    }

    return &Service{
        allowOrigins:        allowOrigins,
        allowMethods:        allowMethods,
        allowHeaders:        allowHeaders,
        exposeHeaders:       exposeHeaders,
        allowCredentials:    config.AllowCredentials,
        maxAge:              config.MaxAge,
        allowOriginFunc:     config.AllowOriginFunc,
        allowMethodsString:  strings.Join(allowMethods, ", "),
        allowHeadersString:  strings.Join(allowHeaders, ", "),
        exposeHeadersString: strings.Join(exposeHeaders, ", "),
        maxAgeString:        strconv.Itoa(config.MaxAge),
    }
}

/* defaultAllowMethodList is the one default the service grants wherever no method preference was expressed: DefaultService and the nil-list fallback of NewService hand out the same list, so narrowing an unrelated field never changes which methods a deployment accepts. */
func defaultAllowMethodList() []string {
    return []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
}

/* defaultAllowHeaderList is the one default header list, Authorization included, shared by DefaultService and the nil-list fallback of NewService. */
func defaultAllowHeaderList() []string {
    return []string{"Origin", "Content-Type", "Accept", "Authorization"}
}

func DefaultService() *Service {
    return NewService(Config{
        AllowOrigins:     []string{"*"},
        AllowMethods:     defaultAllowMethodList(),
        AllowHeaders:     defaultAllowHeaderList(),
        ExposeHeaders:    []string{},
        AllowCredentials: false,
        MaxAge:           86400,
    })
}

func RestrictiveService(allowedOrigins []string) *Service {
    return NewService(Config{
        AllowOrigins:     allowedOrigins,
        AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
        AllowHeaders:     []string{"Content-Type", "Authorization"},
        ExposeHeaders:    []string{},
        AllowCredentials: true,
        MaxAge:           3600,
    })
}

func (instance *Service) AllowOrigins() []string      { return copyStrings(instance.allowOrigins) }
func (instance *Service) AllowMethods() []string      { return copyStrings(instance.allowMethods) }
func (instance *Service) AllowHeaders() []string      { return copyStrings(instance.allowHeaders) }
func (instance *Service) ExposeHeaders() []string     { return copyStrings(instance.exposeHeaders) }
func (instance *Service) AllowCredentials() bool      { return instance.allowCredentials }
func (instance *Service) MaxAge() int                 { return instance.maxAge }
func (instance *Service) AllowMethodsString() string  { return instance.allowMethodsString }
func (instance *Service) AllowHeadersString() string  { return instance.allowHeadersString }
func (instance *Service) ExposeHeadersString() string { return instance.exposeHeadersString }

/* OriginAllowed keeps the scheme-agnostic reading of scheme-less entries deliberately: "example.com" and "*.example.com" admit that host under ANY scheme, "http://" included. It is the short way to admit a development origin served over plaintext, and it withholds exactly the downgrade protection an allowlist usually exists for — that is the trade, and nothing reports it at runtime, so an allowlist that means https writes the scheme out ("https://example.com", "https://*.example.com"), where the scheme IS compared. The frozen majors read entries identically, so a configuration moves between majors without its match changing. */
func (instance *Service) OriginAllowed(origin string) bool {
    if nil != instance.allowOriginFunc {
        return instance.allowOriginFunc(origin)
    }

    normalizedOrigin := normalizeOrigin(origin)
    originHost := extractOriginHost(normalizedOrigin)
    originScheme := extractOriginScheme(normalizedOrigin)

    for _, allowedOrigin := range instance.allowOrigins {
        normalizedAllowedOrigin := strings.TrimSpace(allowedOrigin)
        if "" == normalizedAllowedOrigin {
            continue
        }

        if "*" == normalizedAllowedOrigin {
            return true
        }

        normalizedAllowedOrigin = normalizeOrigin(normalizedAllowedOrigin)

        if true == strings.EqualFold(normalizedOrigin, normalizedAllowedOrigin) {
            return true
        }

        wildcardScheme, wildcardSuffix, isSchemeWildcard := parseSchemeWildcard(normalizedAllowedOrigin)
        if true == isSchemeWildcard {
            if "" == originHost {
                continue
            }

            if false == strings.EqualFold(originScheme, wildcardScheme) {
                continue
            }

            allowedDomain := strings.ToLower(wildcardSuffix)
            if "" == allowedDomain {
                continue
            }

            suffix := "." + allowedDomain
            if true == strings.HasSuffix(originHost, suffix) {
                return true
            }

            continue
        }

        if true == strings.HasPrefix(normalizedAllowedOrigin, "*.") {
            if "" == originHost {
                continue
            }

            allowedDomain := strings.ToLower(strings.TrimPrefix(normalizedAllowedOrigin, "*."))
            if "" == allowedDomain {
                continue
            }

            suffix := "." + allowedDomain
            if true == strings.HasSuffix(originHost, suffix) {
                return true
            }

            continue
        }

        if "" != originHost && strings.ToLower(normalizedAllowedOrigin) == originHost {
            return true
        }
    }

    return false
}

func (instance *Service) ApplyResponseHeaders(origin string, headers nethttp.Header) {
    if nil == headers {
        return
    }

    headers.Set("Access-Control-Allow-Origin", origin)

    if true == instance.allowCredentials {
        headers.Set("Access-Control-Allow-Credentials", "true")
    }

    if "" != instance.exposeHeadersString {
        headers.Set("Access-Control-Expose-Headers", instance.exposeHeadersString)
    }

    addVaryOrigin(headers)
}

func (instance *Service) ApplyPreflightHeaders(origin string, headers nethttp.Header) {
    if nil == headers {
        return
    }

    instance.ApplyResponseHeaders(origin, headers)

    headers.Set("Access-Control-Allow-Methods", instance.allowMethodsString)
    headers.Set("Access-Control-Allow-Headers", instance.allowHeadersString)

    if 0 < instance.maxAge {
        headers.Set("Access-Control-Max-Age", instance.maxAgeString)
    }
}

func (instance *Service) IsPreflight(request httpcontract.Request) bool {
    if true == internal.IsNilInterface(request) || nil == request.HttpRequest() {
        return false
    }

    if nethttp.MethodOptions != request.HttpRequest().Method {
        return false
    }

    return "" != request.HttpRequest().Header.Get("Access-Control-Request-Method")
}

func (instance *Service) RequestOrigin(request httpcontract.Request) string {
    if true == internal.IsNilInterface(request) || nil == request.HttpRequest() {
        return ""
    }

    return request.HttpRequest().Header.Get("Origin")
}

func copyStrings(values []string) []string {
    if nil == values {
        return nil
    }

    return append([]string{}, values...)
}

func normalizeOrigin(origin string) string {
    value := strings.TrimSpace(origin)
    if "" == value {
        return ""
    }

    return strings.TrimSuffix(value, "/")
}

/* extractOriginHost keeps the port the origin names: two origins on different ports of one host are different origins to the browser, so an allow entry without a port grants only the portless spelling — an entry that means to allow a port writes it ("app.example.com:8443"). Reading the host without the port would let any service on another port of an allowed host — a dev server, a legacy admin UI — inherit the grant, with the credentials the restrictive configurations pair with it. */
func extractOriginHost(origin string) string {
    if "" == origin {
        return ""
    }

    parsedUrl, parseErr := url.Parse(origin)
    if nil != parseErr {
        return ""
    }

    host := parsedUrl.Host
    if "" == host {
        return ""
    }

    return strings.ToLower(host)
}

func extractOriginScheme(origin string) string {
    if "" == origin {
        return ""
    }

    parsedUrl, parseErr := url.Parse(origin)
    if nil != parseErr {
        return ""
    }

    return strings.ToLower(parsedUrl.Scheme)
}

/* parseSchemeWildcard recognizes a scheme-qualified wildcard pattern of the form "<scheme>://*.suffix" (for example "https://*.example.com"). It returns the scheme, the subdomain suffix, and true when the pattern is such a wildcard. Scheme-less patterns (for example "*.example.com") are not scheme wildcards and keep their scheme-agnostic host matching. The port is significant in every suffix: a wildcard without one matches only portless origins, and a wildcard meaning to allow a port names it ("https://*.example.com:8443"). */
func parseSchemeWildcard(pattern string) (string, string, bool) {
    index := strings.Index(pattern, "://")
    if -1 == index {
        return "", "", false
    }

    scheme := pattern[:index]
    if "" == scheme {
        return "", "", false
    }

    rest := pattern[index+len("://"):]
    if false == strings.HasPrefix(rest, "*.") {
        return "", "", false
    }

    return scheme, strings.TrimPrefix(rest, "*."), true
}

func addVaryOrigin(headers nethttp.Header) {
    for _, existing := range headers.Values("Vary") {
        for _, token := range strings.Split(existing, ",") {
            if "origin" == strings.ToLower(strings.TrimSpace(token)) {
                return
            }
        }
    }

    headers.Add("Vary", "Origin")
}
