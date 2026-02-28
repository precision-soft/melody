package middleware

import (
    nethttp "net/http"
    "net/url"
    "strconv"
    "strings"

    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"

    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

type CorsConfig struct {
    allowOrigins     []string
    allowMethods     []string
    allowHeaders     []string
    exposeHeaders    []string
    allowCredentials bool
    maxAge           int
    allowOriginFunc  func(origin string) bool
}

func NewCorsConfig(
    allowOrigins []string,
    allowMethods []string,
    allowHeaders []string,
    exposeHeaders []string,
    allowCredentials bool,
    maxAge int,
    allowOriginFunc func(origin string) bool,
) *CorsConfig {
    copiedAllowOrigins := []string{}
    if nil != allowOrigins {
        copiedAllowOrigins = append([]string{}, allowOrigins...)
    }

    copiedAllowMethods := []string{}
    if nil != allowMethods {
        copiedAllowMethods = append([]string{}, allowMethods...)
    }

    copiedAllowHeaders := []string{}
    if nil != allowHeaders {
        copiedAllowHeaders = append([]string{}, allowHeaders...)
    }

    copiedExposeHeaders := []string{}
    if nil != exposeHeaders {
        copiedExposeHeaders = append([]string{}, exposeHeaders...)
    }

    return &CorsConfig{
        allowOrigins:     copiedAllowOrigins,
        allowMethods:     copiedAllowMethods,
        allowHeaders:     copiedAllowHeaders,
        exposeHeaders:    copiedExposeHeaders,
        allowCredentials: allowCredentials,
        maxAge:           maxAge,
        allowOriginFunc:  allowOriginFunc,
    }
}

func (instance *CorsConfig) AllowOrigins() []string {
    if nil == instance.allowOrigins {
        return nil
    }

    return append([]string{}, instance.allowOrigins...)
}

func (instance *CorsConfig) SetAllowOrigins(allowOrigins []string) {
    if nil == allowOrigins {
        instance.allowOrigins = nil
        return
    }

    instance.allowOrigins = append([]string{}, allowOrigins...)
}

func (instance *CorsConfig) AllowMethods() []string {
    if nil == instance.allowMethods {
        return nil
    }

    return append([]string{}, instance.allowMethods...)
}

func (instance *CorsConfig) SetAllowMethods(allowMethods []string) {
    if nil == allowMethods {
        instance.allowMethods = nil
        return
    }

    instance.allowMethods = append([]string{}, allowMethods...)
}

func (instance *CorsConfig) AllowHeaders() []string {
    if nil == instance.allowHeaders {
        return nil
    }

    return append([]string{}, instance.allowHeaders...)
}

func (instance *CorsConfig) SetAllowHeaders(allowHeaders []string) {
    if nil == allowHeaders {
        instance.allowHeaders = nil
        return
    }

    instance.allowHeaders = append([]string{}, allowHeaders...)
}

func (instance *CorsConfig) ExposeHeaders() []string {
    if nil == instance.exposeHeaders {
        return nil
    }

    return append([]string{}, instance.exposeHeaders...)
}

func (instance *CorsConfig) AllowCredentials() bool { return instance.allowCredentials }

func (instance *CorsConfig) MaxAge() int { return instance.maxAge }

func (instance *CorsConfig) AllowOriginFunc() func(origin string) bool {
    return instance.allowOriginFunc
}

func DefaultCorsConfig() *CorsConfig {
    return NewCorsConfig(
        []string{"*"},
        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
        []string{"Origin", "Content-Type", "Accept", "Authorization"},
        []string{},
        false,
        86400,
        nil,
    )
}

func RestrictiveCorsConfig(allowedOrigins []string) *CorsConfig {
    return NewCorsConfig(
        allowedOrigins,
        []string{"GET", "POST", "PUT", "DELETE"},
        []string{"Content-Type", "Authorization"},
        []string{},
        true,
        3600,
        nil,
    )
}

func CorsMiddleware(config *CorsConfig) httpcontract.Middleware {
    if 0 == len(config.AllowOrigins()) {
        config.SetAllowOrigins([]string{"*"})
    }

    if 0 == len(config.AllowMethods()) {
        config.SetAllowMethods([]string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"})
    }

    if 0 == len(config.AllowHeaders()) {
        config.SetAllowHeaders([]string{"Origin", "Content-Type", "Accept"})
    }

    allowMethodsString := strings.Join(config.AllowMethods(), ", ")
    allowHeadersString := strings.Join(config.AllowHeaders(), ", ")
    exposeHeadersString := strings.Join(config.ExposeHeaders(), ", ")
    maxAgeString := strconv.Itoa(config.MaxAge())

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            origin := request.HttpRequest().Header.Get("Origin")

            if "" == origin {
                return next(runtimeInstance, writer, request)
            }

            allowOrigin := isOriginAllowed(origin, config)
            if false == allowOrigin {
                return next(runtimeInstance, writer, request)
            }

            if nethttp.MethodOptions == request.HttpRequest().Method {
                response := http.EmptyResponse(nethttp.StatusNoContent)

                response.Headers().Set("Access-Control-Allow-Origin", origin)
                response.Headers().Set("Access-Control-Allow-Methods", allowMethodsString)
                response.Headers().Set("Access-Control-Allow-Headers", allowHeadersString)

                if 0 < config.MaxAge() {
                    response.Headers().Set("Access-Control-Max-Age", maxAgeString)
                }

                if config.AllowCredentials() {
                    response.Headers().Set("Access-Control-Allow-Credentials", "true")
                }

                if "" != exposeHeadersString {
                    response.Headers().Set("Access-Control-Expose-Headers", exposeHeadersString)
                }

                response.Headers().Set("Vary", "Origin")

                return response, nil
            }

            response, nextMiddlewareErr := next(runtimeInstance, writer, request)
            if nil != nextMiddlewareErr {
                return response, nextMiddlewareErr
            }

            if nil == response {
                response = http.EmptyResponse(nethttp.StatusOK)
            }

            response.Headers().Set("Access-Control-Allow-Origin", origin)

            if config.AllowCredentials() {
                response.Headers().Set("Access-Control-Allow-Credentials", "true")
            }

            if "" != exposeHeadersString {
                response.Headers().Set("Access-Control-Expose-Headers", exposeHeadersString)
            }

            response.Headers().Set("Vary", "Origin")

            return response, nil
        }
    }
}

func DefaultCorsMiddleware() httpcontract.Middleware {
    return CorsMiddleware(DefaultCorsConfig())
}

func RestrictiveCors(allowedOrigins ...string) httpcontract.Middleware {
    return CorsMiddleware(RestrictiveCorsConfig(allowedOrigins))
}

func isOriginAllowed(origin string, config *CorsConfig) bool {
    if nil != config.AllowOriginFunc() {
        return config.AllowOriginFunc()(origin)
    }

    normalizedOrigin := normalizeOrigin(origin)
    originHost := extractOriginHost(normalizedOrigin)

    for _, allowedOrigin := range config.AllowOrigins() {
        normalizedAllowedOrigin := strings.TrimSpace(allowedOrigin)
        if "" == normalizedAllowedOrigin {
            continue
        }

        if "*" == normalizedAllowedOrigin {
            return true
        }

        normalizedAllowedOrigin = normalizeOrigin(normalizedAllowedOrigin)

        if normalizedOrigin == normalizedAllowedOrigin {
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

        if "" != originHost {
            if strings.ToLower(normalizedAllowedOrigin) == originHost {
                return true
            }
        }
    }

    return false
}

func normalizeOrigin(origin string) string {
    value := strings.TrimSpace(origin)
    if "" == value {
        return ""
    }

    value = strings.TrimSuffix(value, "/")

    return value
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
