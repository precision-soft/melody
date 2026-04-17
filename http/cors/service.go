package cors

import (
    nethttp "net/http"
    "net/url"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
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

    if 0 == len(allowOrigins) {
        allowOrigins = []string{"*"}
    }

    if 0 == len(allowMethods) {
        allowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
    }

    if 0 == len(allowHeaders) {
        allowHeaders = []string{"Origin", "Content-Type", "Accept"}
    }

    if true == config.AllowCredentials {
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

func DefaultService() *Service {
    return NewService(Config{
        AllowOrigins:     []string{"*"},
        AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
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

func (instance *Service) OriginAllowed(origin string) bool {
    if nil != instance.allowOriginFunc {
        return instance.allowOriginFunc(origin)
    }

    normalizedOrigin := normalizeOrigin(origin)
    originHost := extractOriginHost(normalizedOrigin)

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
    if nil == request || nil == request.HttpRequest() {
        return false
    }

    return nethttp.MethodOptions == request.HttpRequest().Method
}

func (instance *Service) RequestOrigin(request httpcontract.Request) string {
    if nil == request || nil == request.HttpRequest() {
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

func extractOriginHost(origin string) string {
    if "" == origin {
        return ""
    }

    parsedUrl, parseErr := url.Parse(origin)
    if nil != parseErr {
        return ""
    }

    host := parsedUrl.Hostname()
    if "" == host {
        return ""
    }

    return strings.ToLower(host)
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
