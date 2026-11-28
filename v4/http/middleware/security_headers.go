package middleware

import (
    nethttp "net/http"
    "strings"
    "unicode"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* SecurityHeaderDisabled is the value that turns one header off. */
const SecurityHeaderDisabled = "off"

const (
    DefaultContentTypeOptions      = "nosniff"
    DefaultFrameOptions            = "DENY"
    DefaultReferrerPolicy          = "strict-origin-when-cross-origin"
    DefaultStrictTransportSecurity = "max-age=31536000"
)

/* SecurityHeadersConfig is the security headers every response carries; the zero value sends X-Content-Type-Options, X-Frame-Options and Referrer-Policy, and Strict-Transport-Security on a request resolved to https. A field left empty takes its default, SecurityHeaderDisabled sends nothing for it, any other text is the header's value; a value holding a control character or blank is refused at construction, naming the field. */
type SecurityHeadersConfig struct {
    /* Disabled sends none of the headers. */
    Disabled bool

    /* ContentTypeOptions is X-Content-Type-Options, nosniff by default and the only value admitted. */
    ContentTypeOptions string
    /* FrameOptions is X-Frame-Options, DENY by default; SAMEORIGIN is admitted, any case. */
    FrameOptions string
    /* ReferrerPolicy is Referrer-Policy, strict-origin-when-cross-origin by default. */
    ReferrerPolicy string
    /* StrictTransportSecurity is Strict-Transport-Security, max-age=31536000 by default, sent on https responses only; a value without a max-age directive is refused. */
    StrictTransportSecurity string
    /* ContentSecurityPolicy is Content-Security-Policy, not sent by default. */
    ContentSecurityPolicy string
    /* CrossOriginOpenerPolicy is Cross-Origin-Opener-Policy, not sent by default. */
    CrossOriginOpenerPolicy string
    /* CrossOriginResourcePolicy is Cross-Origin-Resource-Policy, not sent by default. */
    CrossOriginResourcePolicy string
    /* PermissionsPolicy is Permissions-Policy, not sent by default. */
    PermissionsPolicy string
}

type securityHeaderValue struct {
    name  string
    value string
}

/* SecurityHeaders is the resolved set of security headers a response carries. */
type SecurityHeaders struct {
    values []securityHeaderValue
}

/* NewSecurityHeaders resolves the configuration, panicking on a value the browsers would misread, naming the field. */
func NewSecurityHeaders(config SecurityHeadersConfig) *SecurityHeaders {
    fields := []struct {
        field        string
        name         string
        value        string
        defaultValue string
    }{
        {field: "ContentTypeOptions", name: http.HeaderContentTypeOptions, value: config.ContentTypeOptions, defaultValue: DefaultContentTypeOptions},
        {field: "FrameOptions", name: http.HeaderFrameOptions, value: config.FrameOptions, defaultValue: DefaultFrameOptions},
        {field: "ReferrerPolicy", name: http.HeaderReferrerPolicy, value: config.ReferrerPolicy, defaultValue: DefaultReferrerPolicy},
        {field: "StrictTransportSecurity", name: http.HeaderStrictTransportSecurity, value: config.StrictTransportSecurity, defaultValue: DefaultStrictTransportSecurity},
        {field: "ContentSecurityPolicy", name: http.HeaderContentSecurityPolicy, value: config.ContentSecurityPolicy},
        {field: "CrossOriginOpenerPolicy", name: http.HeaderCrossOriginOpenerPolicy, value: config.CrossOriginOpenerPolicy},
        {field: "CrossOriginResourcePolicy", name: http.HeaderCrossOriginResourcePolicy, value: config.CrossOriginResourcePolicy},
        {field: "PermissionsPolicy", name: http.HeaderPermissionsPolicy, value: config.PermissionsPolicy},
    }

    securityHeaders := &SecurityHeaders{values: make([]securityHeaderValue, 0, len(fields))}

    for _, current := range fields {
        value := resolveSecurityHeaderValue(current.field, current.name, current.value, current.defaultValue)
        if "" == value || true == config.Disabled {
            continue
        }

        securityHeaders.values = append(securityHeaders.values, securityHeaderValue{name: current.name, value: value})
    }

    return securityHeaders
}

/* resolveSecurityHeaderValue answers the value sent for one header, "" for none */
func resolveSecurityHeaderValue(field string, name string, value string, defaultValue string) string {
    if "" == value {
        return defaultValue
    }

    if true == strings.EqualFold(SecurityHeaderDisabled, value) {
        return ""
    }

    if "" == strings.TrimSpace(value) || true == strings.ContainsFunc(value, unicode.IsControl) {
        refuseSecurityHeaderValue("a security header value may not hold a control character or be blank", field, value)
    }

    switch name {
    case http.HeaderContentTypeOptions:
        if false == strings.EqualFold(DefaultContentTypeOptions, value) {
            refuseSecurityHeaderValue("X-Content-Type-Options admits nosniff only", field, value)
        }

        return DefaultContentTypeOptions
    case http.HeaderFrameOptions:
        upperValue := strings.ToUpper(value)
        if DefaultFrameOptions != upperValue && "SAMEORIGIN" != upperValue {
            refuseSecurityHeaderValue("X-Frame-Options admits DENY or SAMEORIGIN", field, value)
        }

        return upperValue
    case http.HeaderStrictTransportSecurity:
        if false == carriesMaxAgeDirective(value) {
            refuseSecurityHeaderValue("Strict-Transport-Security needs a max-age directive", field, value)
        }
    }

    return value
}

func carriesMaxAgeDirective(value string) bool {
    for _, directive := range strings.Split(value, ";") {
        directiveName, directiveValue, hasValue := strings.Cut(strings.TrimSpace(directive), "=")
        if false == hasValue || false == strings.EqualFold("max-age", strings.TrimSpace(directiveName)) {
            continue
        }

        digits := strings.Trim(strings.TrimSpace(directiveValue), "\"")
        if "" == digits {
            return false
        }

        for _, character := range digits {
            if '0' > character || '9' < character {
                return false
            }
        }

        return true
    }

    return false
}

func refuseSecurityHeaderValue(message string, field string, value string) {
    exception.Panic(
        exception.NewError(
            message,
            exceptioncontract.Context{
                "field": "SecurityHeadersConfig." + field,
                "value": internal.BoundDiagnosticText(value),
            },
            nil,
        ),
    )
}

/* Apply sets each security header the headers do not already carry, so a handler's own value wins; it leaves out the headers the matched route turned off and sends Strict-Transport-Security only when the request resolved to https. */
func (instance *SecurityHeaders) Apply(headers nethttp.Header, request httpcontract.Request) {
    if nil == headers || 0 == len(instance.values) {
        return
    }

    https := false
    disabledNames := []string(nil)

    if false == internal.IsNilInterface(request) && false == internal.IsNilInterface(request.Attributes()) {
        if schemeValue, published := request.Attributes().Get(http.RequestAttributeScheme); true == published {
            scheme, _ := schemeValue.(string)
            https = true == strings.EqualFold("https", scheme)
        }

        disabledNames = http.RouteProtection(request).SecurityHeadersDisabled
    }

    for _, current := range instance.values {
        if http.HeaderStrictTransportSecurity == current.name && false == https {
            continue
        }

        if true == securityHeaderNamed(disabledNames, current.name) {
            continue
        }

        if 0 < len(headers.Values(current.name)) {
            continue
        }

        headers.Set(current.name, current.value)
    }
}

func securityHeaderNamed(names []string, name string) bool {
    for _, candidate := range names {
        if true == strings.EqualFold(name, strings.TrimSpace(candidate)) {
            return true
        }
    }

    return false
}

/* Middleware applies the headers to the writer before the handler runs, so a stream or a served file that commits its own headers carries them, and to the response the chain returns; a response a listener produced never enters the chain and is decorated by the kernel.response listener. */
func (instance *SecurityHeaders) Middleware() httpcontract.Middleware {
    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            instance.Apply(writer.Header(), request)

            response, nextErr := next(runtimeInstance, writer, request)
            /* a typed nil is handed on as nil, so the kernel answers its empty 204 */
            if true == internal.IsNilInterface(response) {
                return nil, nextErr
            }

            if nil == response.Headers() {
                response.SetHeaders(make(nethttp.Header))
            }

            instance.Apply(response.Headers(), request)

            return response, nextErr
        }
    }
}
