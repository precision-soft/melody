package security

import (
    "errors"
    "mime"
    nethttp "net/http"
    "net/url"
    "strings"
    "unicode"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* CsrfMode is how a firewall checks an unsafe request that rides its session for forgery. */
type CsrfMode string

const (
    /* CsrfModeOrigin admits a request the browser states as same-origin and asks the token of one that states nothing; what a mode left empty resolves to. */
    CsrfModeOrigin CsrfMode = "origin"

    /* CsrfModeToken asks the token of every unsafe request that rides a non-empty session, same-origin ones included. */
    CsrfModeToken CsrfMode = "token"

    /* CsrfModeDisabled checks nothing. */
    CsrfModeDisabled CsrfMode = "disabled"
)

const (
    CsrfReasonCrossSite    = "csrf_cross_site"
    CsrfReasonTokenMissing = "csrf_token_missing"
    CsrfReasonTokenInvalid = "csrf_token_invalid"
)

/* ErrCsrfRefused is the cause of every 403 the csrf check answers, so an application's kernel.exception listener can render it. */
var ErrCsrfRefused = errors.New("csrf refused")

/* CsrfConfig is how a firewall checks for forgery; the zero value checks in CsrfModeOrigin under http.DefaultCsrfHeaderName and http.DefaultCsrfFieldName, trusting no other origin. */
type CsrfConfig struct {
    Mode CsrfMode

    /* HeaderName is the header the token is read from first; an http token. */
    HeaderName string

    /* FieldName is the urlencoded form field the token is read from when the header is absent; never a multipart field, the query or a json body. */
    FieldName string

    /* TrustedOrigins are other origins read as the request's own, each scheme://host[:port], the scheme and the port exact, the host in any case. */
    TrustedOrigins []string
}

/* ResolveCsrfConfig answers the configuration with every empty field at its default, or the refusal naming the field under fieldPrefix. */
func ResolveCsrfConfig(config CsrfConfig, fieldPrefix string) (CsrfConfig, error) {
    resolved := CsrfConfig{
        Mode:           config.Mode,
        HeaderName:     config.HeaderName,
        FieldName:      config.FieldName,
        TrustedOrigins: append([]string{}, config.TrustedOrigins...),
    }

    switch resolved.Mode {
    case "":
        resolved.Mode = CsrfModeOrigin
    case CsrfModeOrigin, CsrfModeToken, CsrfModeDisabled:
    default:
        return CsrfConfig{}, csrfConfigRefusal("unknown security csrf mode", fieldPrefix+".Mode", string(config.Mode))
    }

    if "" == resolved.HeaderName {
        resolved.HeaderName = http.DefaultCsrfHeaderName
    } else if false == isHttpToken(resolved.HeaderName) {
        return CsrfConfig{}, csrfConfigRefusal("a security csrf header name must be an http token", fieldPrefix+".HeaderName", config.HeaderName)
    }

    if "" == resolved.FieldName {
        resolved.FieldName = http.DefaultCsrfFieldName
    } else if "" == strings.TrimSpace(resolved.FieldName) || true == strings.ContainsFunc(resolved.FieldName, unicode.IsControl) {
        return CsrfConfig{}, csrfConfigRefusal("a security csrf field name may not hold a control character", fieldPrefix+".FieldName", config.FieldName)
    }

    for index, origin := range resolved.TrustedOrigins {
        if false == isOriginForm(origin) {
            refusal := csrfConfigRefusal("a security csrf trusted origin must be scheme://host[:port]", fieldPrefix+".TrustedOrigins", origin)
            refusal.SetContextValue("index", index)

            return CsrfConfig{}, refusal
        }
    }

    return resolved, nil
}

func csrfConfigRefusal(message string, field string, value string) *exception.Error {
    return exception.NewError(
        message,
        exceptioncontract.Context{
            "field": field,
            "value": internal.BoundDiagnosticText(value),
        },
        nil,
    )
}

func isHttpToken(value string) bool {
    if "" == value {
        return false
    }

    for _, character := range value {
        if ('a' <= character && 'z' >= character) || ('A' <= character && 'Z' >= character) || ('0' <= character && '9' >= character) {
            continue
        }

        if false == strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
            return false
        }
    }

    return true
}

func isOriginForm(origin string) bool {
    if "" == origin || true == strings.ContainsAny(origin, "*") || true == strings.ContainsFunc(origin, unicode.IsSpace) {
        return false
    }

    parsed, parseErr := url.Parse(origin)
    if nil != parseErr {
        return false
    }

    return "" != parsed.Scheme && "" != parsed.Host && nil == parsed.User && "" == parsed.Path && "" == parsed.RawQuery && "" == parsed.Fragment && "" == parsed.Opaque && false == parsed.ForceQuery
}

/* RequestIsCrossSite reads what a browser states about the request's origin: Sec-Fetch-Site when it sent one, where only same-origin and a user-initiated none pass, so a sibling subdomain is refused, else Origin against the request's scheme and host, so a plain http page cannot post to its https twin. A request stating neither is not read as cross-site. */
func RequestIsCrossSite(request httpcontract.Request) bool {
    _, crossSite := requestOriginStatement(request, nil)

    return crossSite
}

/* requestOriginStatement answers whether the browser stated an origin and whether it is another one than the request's own or a trusted one */
func requestOriginStatement(request httpcontract.Request, trustedOrigins []string) (bool, bool) {
    httpRequest := request.HttpRequest()

    origin := strings.TrimSpace(httpRequest.Header.Get("Origin"))
    fetchSite := strings.ToLower(strings.TrimSpace(httpRequest.Header.Get("Sec-Fetch-Site")))

    if "" == fetchSite && "" == origin {
        return false, false
    }

    if "" != origin && true == originIsTrusted(origin, trustedOrigins) {
        return true, false
    }

    if "" != fetchSite {
        return true, "same-origin" != fetchSite && "none" != fetchSite
    }

    /* "null", an opaque origin, parses to no host and is refused with any foreign one */
    originUrl, parseErr := url.Parse(origin)
    if nil != parseErr {
        return true, true
    }

    if schemeValue, published := request.Attributes().Get(http.RequestAttributeScheme); true == published {
        if scheme, isText := schemeValue.(string); true == isText && false == strings.EqualFold(originUrl.Scheme, scheme) {
            return true, true
        }
    }

    return true, false == strings.EqualFold(originUrl.Host, httpRequest.Host)
}

func originIsTrusted(origin string, trustedOrigins []string) bool {
    if 0 == len(trustedOrigins) {
        return false
    }

    originUrl, parseErr := url.Parse(origin)
    if nil != parseErr || "" == originUrl.Host || "" != originUrl.Path {
        return false
    }

    for _, trustedOrigin := range trustedOrigins {
        trustedUrl, trustedErr := url.Parse(trustedOrigin)
        if nil != trustedErr {
            continue
        }

        if trustedUrl.Scheme == originUrl.Scheme && true == strings.EqualFold(trustedUrl.Host, originUrl.Host) {
            return true
        }
    }

    return false
}

/* refuseForgedRequest checks a request the firewall matched and no door answered: a firewall that checks nothing, a route that turned the check off, a safe method and a request presenting no session cookie pass; a stated other origin is refused; a stated same origin passes in CsrfModeOrigin; anything else over a session holding more than the secret must present a token that verifies */
func refuseForgedRequest(
    runtimeInstance containercontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
) error {
    if true == firewall.CsrfExempt() {
        return nil
    }

    request := requestEvent.Request()
    if true == http.RouteProtection(request).CsrfDisabled {
        return nil
    }

    return checkForgedRequest(runtimeInstance, requestEvent, firewall, request.RouteName())
}

/* refuseForgedDoorRequest checks a door's request: a stated other origin is refused without a session cookie and on a stateless firewall too, since a sign-in is where a credential is exchanged */
func refuseForgedDoorRequest(
    runtimeInstance containercontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
    door firewallDoorSettings,
) error {
    if true == door.csrfDisabled || CsrfModeDisabled == firewall.csrf.Mode {
        return nil
    }

    request := requestEvent.Request()
    if _, crossSite := requestOriginStatement(request, firewall.csrf.TrustedOrigins); true == crossSite {
        return newCsrfRefusal(firewall, request, "cross-site request refused", CsrfReasonCrossSite)
    }

    if true == firewall.stateless {
        return nil
    }

    return checkForgedRequest(runtimeInstance, requestEvent, firewall, door.name)
}

func checkForgedRequest(
    runtimeInstance containercontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
    entryName string,
) error {
    request := requestEvent.Request()
    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return nil
    }

    if true == http.IsSafeMethod(httpRequest.Method) {
        return nil
    }

    if false == http.RequestCarriesSessionCookie(request) {
        return nil
    }

    stated, crossSite := requestOriginStatement(request, firewall.csrf.TrustedOrigins)
    if true == crossSite {
        return newCsrfRefusal(firewall, request, "cross-site request refused", CsrfReasonCrossSite)
    }

    if true == stated && CsrfModeOrigin == firewall.csrf.Mode {
        return nil
    }

    sessionInstance, sessionErr := requestSessionAtFirewallDoor(request)
    if nil != sessionErr {
        return refuseSessionErrorAtFirewallDoor(runtimeInstance, requestEvent, firewall, entryName, sessionErr)
    }

    /* the secret alone is what the token route writes into an anonymous session, which must not turn a sign-in into a request demanding the token */
    holdsOtherValue := false
    for key := range sessionInstance.All() {
        if http.SessionKeyCsrf != key {
            holdsOtherValue = true

            break
        }
    }

    if false == holdsOtherValue {
        return nil
    }

    presented, formErr := presentedCsrfToken(request, firewall.csrf)
    if nil != formErr {
        /* a body past its cap or one that does not parse is refused as malformed before it is judged forged */
        return formErr
    }

    if "" == presented {
        return newCsrfRefusal(firewall, request, "csrf token refused", CsrfReasonTokenMissing)
    }

    verified, verifyErr := http.VerifyCsrfToken(request, presented)
    if nil != verifyErr {
        return refuseSessionErrorAtFirewallDoor(runtimeInstance, requestEvent, firewall, entryName, verifyErr)
    }

    if false == verified {
        return newCsrfRefusal(firewall, request, "csrf token refused", CsrfReasonTokenInvalid)
    }

    return nil
}

/* presentedCsrfToken reads the header, else the field of a urlencoded body, through the request's form read bounded by the route's cap, whose failure it answers; a multipart body is never read, so an upload is not buffered before authorization */
func presentedCsrfToken(request httpcontract.Request, config CsrfConfig) (string, error) {
    httpRequest := request.HttpRequest()

    if headerToken := strings.TrimSpace(httpRequest.Header.Get(config.HeaderName)); "" != headerToken {
        return headerToken, nil
    }

    mediaType, _, parseErr := mime.ParseMediaType(httpRequest.Header.Get("Content-Type"))
    if nil != parseErr || "application/x-www-form-urlencoded" != mediaType {
        return "", nil
    }

    postBag, formErr := request.Post()
    if nil != formErr {
        return "", formErr
    }

    if true == internal.IsNilInterface(postBag) {
        return "", nil
    }

    value, exists := postBag.Get(config.FieldName)
    if false == exists {
        return "", nil
    }

    fieldToken, isText := value.(string)
    if false == isText {
        return "", nil
    }

    return strings.TrimSpace(fieldToken), nil
}

func newCsrfRefusal(firewall *CompiledFirewall, request httpcontract.Request, message string, reason string) error {
    httpRequest := request.HttpRequest()

    refusal := exception.NewHttpError(
        nethttp.StatusForbidden,
        exception.HttpErrorConfig{Message: message, Cause: ErrCsrfRefused},
    )
    refusal.SetContextValue("reason", reason)
    refusal.SetContextValue("firewallName", firewall.Name())
    refusal.SetContextValue("csrfMode", string(firewall.csrf.Mode))
    refusal.SetContextValue("fetchSite", internal.BoundDiagnosticText(httpRequest.Header.Get("Sec-Fetch-Site")))
    refusal.SetContextValue("origin", internal.BoundDiagnosticText(httpRequest.Header.Get("Origin")))

    return refusal
}
