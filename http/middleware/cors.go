package middleware

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/http/cors"
)

// Deprecated: use github.com/precision-soft/melody/http/cors.Service instead.
type CorsConfig struct {
    allowOrigins     []string
    allowMethods     []string
    allowHeaders     []string
    exposeHeaders    []string
    allowCredentials bool
    maxAge           int
    allowOriginFunc  func(origin string) bool
}

// Deprecated: use github.com/precision-soft/melody/http/cors.NewService instead.
func NewCorsConfig(
    allowOrigins []string,
    allowMethods []string,
    allowHeaders []string,
    exposeHeaders []string,
    allowCredentials bool,
    maxAge int,
    allowOriginFunc func(origin string) bool,
) *CorsConfig {
    return &CorsConfig{
        allowOrigins:     copyStringsForCors(allowOrigins),
        allowMethods:     copyStringsForCors(allowMethods),
        allowHeaders:     copyStringsForCors(allowHeaders),
        exposeHeaders:    copyStringsForCors(exposeHeaders),
        allowCredentials: allowCredentials,
        maxAge:           maxAge,
        allowOriginFunc:  allowOriginFunc,
    }
}

func (instance *CorsConfig) AllowOrigins() []string {
    return copyStringsForCors(instance.allowOrigins)
}

func (instance *CorsConfig) SetAllowOrigins(allowOrigins []string) {
    instance.allowOrigins = copyStringsForCors(allowOrigins)
}

func (instance *CorsConfig) AllowMethods() []string {
    return copyStringsForCors(instance.allowMethods)
}

func (instance *CorsConfig) SetAllowMethods(allowMethods []string) {
    instance.allowMethods = copyStringsForCors(allowMethods)
}

func (instance *CorsConfig) AllowHeaders() []string {
    return copyStringsForCors(instance.allowHeaders)
}

func (instance *CorsConfig) SetAllowHeaders(allowHeaders []string) {
    instance.allowHeaders = copyStringsForCors(allowHeaders)
}

func (instance *CorsConfig) ExposeHeaders() []string {
    return copyStringsForCors(instance.exposeHeaders)
}

func (instance *CorsConfig) AllowCredentials() bool {
    return instance.allowCredentials
}

func (instance *CorsConfig) MaxAge() int {
    return instance.maxAge
}

func (instance *CorsConfig) AllowOriginFunc() func(origin string) bool {
    return instance.allowOriginFunc
}

// Deprecated: use github.com/precision-soft/melody/http/cors.DefaultService instead.
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

// Deprecated: use github.com/precision-soft/melody/http/cors.RestrictiveService instead.
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

// Deprecated: use github.com/precision-soft/melody/http/cors.Middleware instead.
func CorsMiddleware(config *CorsConfig) httpcontract.Middleware {
    service := cors.NewService(cors.Config{
        AllowOrigins:     config.allowOrigins,
        AllowMethods:     config.allowMethods,
        AllowHeaders:     config.allowHeaders,
        ExposeHeaders:    config.exposeHeaders,
        AllowCredentials: config.allowCredentials,
        MaxAge:           config.maxAge,
        AllowOriginFunc:  config.allowOriginFunc,
    })

    return cors.Middleware(service)
}

// Deprecated: use github.com/precision-soft/melody/http/cors.DefaultMiddleware instead.
func DefaultCorsMiddleware() httpcontract.Middleware {
    return cors.DefaultMiddleware()
}

// Deprecated: use github.com/precision-soft/melody/http/cors.Restrictive instead.
func RestrictiveCors(allowedOrigins ...string) httpcontract.Middleware {
    return cors.Restrictive(allowedOrigins...)
}

func copyStringsForCors(values []string) []string {
    if nil == values {
        return nil
    }

    return append([]string{}, values...)
}
