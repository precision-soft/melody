package security

import (
    "errors"
    nethttp "net/http"
    "net/url"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* CrossSitePolicy states what a firewall door does with a request a browser marks as coming from another site. */
type CrossSitePolicy string

const (
    /* CrossSiteRefuse answers 403 to it, and is what a door left unset does. */
    CrossSiteRefuse CrossSitePolicy = "refuse"

    /* CrossSiteAdmit serves it, for a sign-in an api deliberately calls across origins under its CORS policy. */
    CrossSiteAdmit CrossSitePolicy = "admit"

    /* DefaultFirewallDoorTargetPath is where an html client is redirected when a door's handler answers no response. */
    DefaultFirewallDoorTargetPath = "/"
)

/* ErrSecondFactorRequired is the cause of the 401 a login door answers to accepted credentials whose second factor was not presented, so an application's kernel.exception listener can render the challenge. */
var ErrSecondFactorRequired = errors.New("second factor required")

/* DefaultFirewallDoorMethods answers the methods a door serves when it declares none. */
func DefaultFirewallDoorMethods() []string {
    return []string{nethttp.MethodPost}
}

/* FirewallDoor is a door's public declaration, what the application publishes as a route definition and checks the registered routes against. */
type FirewallDoor struct {
    Name       string
    Path       string
    Methods    []string
    Attributes map[string]any
}

type firewallDoorSettings struct {
    name       string
    attributes map[string]any
    methods    []string
    targetPath string
    crossSite  CrossSitePolicy
}

func doorMethodsOrDefault(methods []string) []string {
    if 0 == len(methods) {
        return DefaultFirewallDoorMethods()
    }

    return append([]string{}, methods...)
}

func doorTargetPathOrDefault(targetPath string) string {
    if "" == targetPath {
        return DefaultFirewallDoorTargetPath
    }

    return targetPath
}

/* Doors answers the login and the logout door the firewall declares. */
func (instance *CompiledFirewall) Doors() []FirewallDoor {
    doors := make([]FirewallDoor, 0, 2)

    if "" != instance.loginPath {
        doors = append(doors, instance.loginDoor.publicForm(instance.loginPath))
    }

    if "" != instance.logoutPath {
        doors = append(doors, instance.logoutDoor.publicForm(instance.logoutPath))
    }

    return doors
}

func (instance firewallDoorSettings) publicForm(path string) FirewallDoor {
    return FirewallDoor{
        Name:       instance.name,
        Path:       path,
        Methods:    append([]string{}, instance.methods...),
        Attributes: internal.CopyAnyMap(instance.attributes),
    }
}

func (instance firewallDoorSettings) serves(method string) bool {
    for _, doorMethod := range instance.methods {
        if doorMethod == method {
            return true
        }
    }

    return false
}

/* FirewallDoorPathForm is a path as a door compares it: the router reads "/login/" and "/login" as one route. */
func FirewallDoorPathForm(path string) string {
    if 1 >= len(path) {
        return path
    }

    trimmedPath := strings.TrimRight(path, "/")
    if "" == trimmedPath {
        return "/"
    }

    return trimmedPath
}

/* serveFirewallDoor answers a request on one of the firewall's doors; any other request passes on to the rest of the chain. A door is served before access control, which stands aside on what it answered. */
func serveFirewallDoor(
    runtimeInstance runtimecontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
    token securitycontract.Token,
) {
    if "" == firewall.loginPath && "" == firewall.logoutPath {
        return
    }

    httpRequest := requestEvent.Request().HttpRequest()
    if nil == httpRequest || nil == httpRequest.URL {
        return
    }

    /* the spelling the router matched, not the decoded URL.Path, so "%2F" cannot alias a door */
    path := FirewallDoorPathForm(http.RequestPathAsRouted(internal.RequestPathAsSent(httpRequest.URL)))

    if "" != firewall.loginPath && FirewallDoorPathForm(firewall.loginPath) == path && true == firewall.loginDoor.serves(httpRequest.Method) {
        serveLoginDoor(runtimeInstance, requestEvent, firewall, token)

        return
    }

    if "" != firewall.logoutPath && FirewallDoorPathForm(firewall.logoutPath) == path && true == firewall.logoutDoor.serves(httpRequest.Method) {
        serveLogoutDoor(runtimeInstance, requestEvent, firewall, token)
    }
}

func serveLoginDoor(
    runtimeInstance runtimecontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
    token securitycontract.Token,
) {
    request := requestEvent.Request()

    if nil != firewall.loginRateLimit && true == firewall.loginRateLimit.Apply(runtimeInstance, requestEvent) {
        return
    }

    if CrossSiteAdmit != firewall.loginDoor.crossSite && true == requestIsCrossSite(request) {
        requestEvent.Refuse(exception.Forbidden("cross-site request refused"))

        return
    }

    /* without an authenticator the handler reads the source's token, and would answer "invalid credentials" for a credential nobody could check */
    if true == internal.IsNilInterface(firewall.loginAuthenticator) {
        if cause, unavailable := AuthenticationUnavailableCause(token); true == unavailable {
            if true == exception.ChainIs(cause, session.ErrSessionUnavailable) {
                requestEvent.Refuse(newSessionUnavailableRefusal(firewall, cause))

                return
            }

            requestEvent.Refuse(newAuthenticationUnavailableRefusal(firewall, cause))

            return
        }
    }

    previousSessionId := ""

    /* read before the authenticator and the handler, so a sign-in the session storage cannot complete checks no credential, spends no second factor and writes no identity to a session nothing stores */
    sessionInstance, sessionErr := requestSessionAtFirewallDoor(request)
    if nil != sessionErr {
        requestEvent.Refuse(refuseSessionErrorAtFirewallDoor(runtimeInstance, requestEvent, firewall, firewall.loginDoor.name, sessionErr))

        return
    }

    sessionIdAtDoor := sessionInstance.Id()

    if false == internal.IsNilInterface(firewall.loginAuthenticator) {
        authenticatedToken, refusal := authenticateAtLoginDoor(runtimeInstance, requestEvent, firewall)
        if nil != refusal {
            requestEvent.Refuse(refusal)

            return
        }

        token = authenticatedToken

        /* rotated before the handler writes the identity: an id the client held before signing in must not carry it */
        previousSessionId = sessionIdAtDoor

        if _, regenerateErr := http.RegenerateRequestSession(request); nil != regenerateErr {
            requestEvent.Refuse(refuseSessionErrorAtFirewallDoor(runtimeInstance, requestEvent, firewall, firewall.loginDoor.name, regenerateErr))

            return
        }
    }

    result, loginErr := firewall.Login(runtimeInstance, request, securitycontract.LoginInput{Token: token, PreviousSessionId: previousSessionId})
    if nil != loginErr {
        /* a session rotated since the door read it, by the door or by the handler, may carry the identity, and a refused sign-in must not leave it usable */
        if currentSession, currentSessionErr := http.RequestSession(request); nil == currentSessionErr && sessionIdAtDoor != currentSession.Id() {
            currentSession.Clear()
        }

        /* a handler that rotated the session itself, the door without an authenticator, meets a session outage under its own reason */
        if true == exception.ChainIs(loginErr, session.ErrSessionUnavailable) {
            requestEvent.Refuse(refuseSessionErrorAtFirewallDoor(runtimeInstance, requestEvent, firewall, firewall.loginDoor.name, loginErr))

            return
        }

        if true == exception.AnswersUnavailable(loginErr) {
            requestEvent.Refuse(refuseUnavailableAtLoginDoor(runtimeInstance, requestEvent, firewall, loginErr))

            return
        }

        requestEvent.Refuse(loginErr)

        return
    }

    setSecurityContextOnRuntime(runtimeInstance, firewall, result.Token)

    answerFirewallDoor(
        requestEvent,
        result.Response,
        firewall.loginDoor.targetPath,
        func() (httpcontract.Response, error) {
            return http.JsonResponse(nethttp.StatusOK, map[string]any{"authenticated": true})
        },
    )
}

/* authenticateAtLoginDoor reads the credentials through the door's authenticator, on the token source's terms: refused credentials and a refused second factor raise security.login.failure, a second factor not yet presented is a challenge announced to nobody, and an authenticator's error is a failed sign-in only when it answers 401 or 403, a refused credential, an outage or a malformed request being none. An outage, a marked error with no http status around the mark or a token carrying securitycontract.AuthenticationUnavailable, is refused 503 with the firewall's Retry-After. */
func authenticateAtLoginDoor(
    runtimeInstance runtimecontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
) (securitycontract.Token, error) {
    request := requestEvent.Request()

    var token securitycontract.Token = NewAnonymousToken()

    if true == firewall.loginAuthenticator.Supports(request) {
        authenticatedToken, authenticateErr := firewall.loginAuthenticator.Authenticate(request)
        if nil != authenticateErr {
            if true == exception.AnswersUnavailable(authenticateErr) {
                return nil, refuseUnavailableAtLoginDoor(runtimeInstance, requestEvent, firewall, authenticateErr)
            }

            if true == raisesLoginFailure(authenticateErr) {
                fileLoginFailureDispatchError(
                    runtimeInstance,
                    firewall.dispatchLoginFailure(runtimeInstance, request, authenticateErr),
                    exception.AsHttpException(authenticateErr).StatusCode(),
                    exceptioncontract.Context{"firewallName": firewall.Name()},
                )
            }

            return nil, authenticateErr
        }

        /* a typed nil token is how "no user" is written by hand */
        if false == internal.IsNilInterface(authenticatedToken) {
            token = authenticatedToken
        }
    }

    if cause, unavailable := AuthenticationUnavailableCause(token); true == unavailable {
        return nil, refuseUnavailableAtLoginDoor(runtimeInstance, requestEvent, firewall, cause)
    }

    if true == token.IsAuthenticated() {
        return token, nil
    }

    if _, isPending := token.(securitycontract.TwoFactorPending); true == isPending {
        rejection, reportsRejection := token.(securitycontract.TwoFactorRejection)
        if false == reportsRejection || false == rejection.SecondFactorRejected() {
            return nil, exception.NewHttpError(
                nethttp.StatusUnauthorized,
                exception.HttpErrorConfig{Message: ErrSecondFactorRequired.Error(), Cause: ErrSecondFactorRequired},
            )
        }
    }

    failureErr := errors.New("invalid credentials")

    fileLoginFailureDispatchError(
        runtimeInstance,
        firewall.dispatchLoginFailure(runtimeInstance, request, failureErr),
        nethttp.StatusUnauthorized,
        exceptioncontract.Context{"firewallName": firewall.Name()},
    )

    return nil, exception.Unauthorized(failureErr.Error())
}

/* refuseUnavailableAtLoginDoor files the one record of a credential the door could not check, since no resolution listener saw it, unless the cause was already filed */
func refuseUnavailableAtLoginDoor(
    runtimeInstance runtimecontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
    cause error,
) error {
    if false == exception.IsAlreadyLogged(cause) {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Error(
                "security login door authenticator could not verify the credential: its platform is unavailable",
                resolutionLogContext(
                    cause,
                    requestEvent,
                    exceptioncontract.Context{
                        "firewallName": firewall.Name(),
                        "doorName":     firewall.loginDoor.name,
                    },
                ),
            )
        }
    }

    return newAuthenticationUnavailableRefusal(firewall, cause)
}

/* requestSessionAtFirewallDoor reads the request's session for a door and answers an unavailable session as the outage it stands for, since a sign-in has nowhere to put the identity and a sign-out cannot end what the storage cannot reach */
func requestSessionAtFirewallDoor(request httpcontract.Request) (sessioncontract.Session, error) {
    sessionInstance, sessionErr := http.RequestSession(request)
    if nil != sessionErr {
        return nil, sessionErr
    }

    if cause, isUnavailable := session.SessionUnavailableCause(sessionInstance); true == isUnavailable {
        /* an unavailable session an application built over an unmarked cause is still an outage at the door */
        if false == exception.AnswersUnavailable(cause) {
            cause = exception.MarkInfrastructureFailure(cause)
        }

        return nil, cause
    }

    return sessionInstance, nil
}

/* refuseSessionErrorAtFirewallDoor answers a session a door could not load or rotate: a storage that could not answer is refused 503 with the firewall's Retry-After and reason session_unavailable, its cause filed once, and any other error is refused as it is */
func refuseSessionErrorAtFirewallDoor(
    runtimeInstance runtimecontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
    doorName string,
    sessionErr error,
) error {
    if false == exception.AnswersUnavailable(sessionErr) {
        return sessionErr
    }

    if false == exception.IsAlreadyLogged(sessionErr) {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Error(
                "security firewall door could not read or rotate the session: its storage is unavailable",
                resolutionLogContext(
                    sessionErr,
                    requestEvent,
                    exceptioncontract.Context{
                        "firewallName": firewall.Name(),
                        "doorName":     doorName,
                    },
                ),
            )
        }
    }

    return newSessionUnavailableRefusal(firewall, sessionErr)
}

func serveLogoutDoor(
    runtimeInstance runtimecontract.Runtime,
    requestEvent *http.KernelRequestEvent,
    firewall *CompiledFirewall,
    token securitycontract.Token,
) {
    request := requestEvent.Request()

    if CrossSiteAdmit != firewall.logoutDoor.crossSite && true == requestIsCrossSite(request) {
        requestEvent.Refuse(exception.Forbidden("cross-site request refused"))

        return
    }

    /* read before the handler: a session the storage cannot answer cannot be ended, so nothing is cleared and the client is told to come back */
    sessionInstance, sessionErr := requestSessionAtFirewallDoor(request)
    if nil != sessionErr {
        requestEvent.Refuse(refuseSessionErrorAtFirewallDoor(runtimeInstance, requestEvent, firewall, firewall.logoutDoor.name, sessionErr))

        return
    }

    result, logoutErr := firewall.Logout(runtimeInstance, request, securitycontract.LogoutInput{Token: token})

    /* a result beside an error is a handler that succeeded and a success listener that failed: the sign-out stands, so the session ends either way, the one a handler rotated or published included. Clear routes the response path to deleting the entry and expiring the cookie. */
    if nil != result {
        sessionInstance.Clear()

        if publishedSession, publishedErr := http.RequestSession(request); nil == publishedErr && publishedSession != sessionInstance {
            publishedSession.Clear()
        }

        setSecurityContextOnRuntime(runtimeInstance, firewall, NewAnonymousToken())
    }

    if nil != logoutErr {
        if nil != result {
            http.CloseDiscardedResponseBody(result.Response, logging.LoggerFromRuntime(runtimeInstance))
        }

        requestEvent.Refuse(logoutErr)

        return
    }

    answerFirewallDoor(
        requestEvent,
        result.Response,
        firewall.logoutDoor.targetPath,
        func() (httpcontract.Response, error) {
            return http.EmptyResponse(nethttp.StatusNoContent), nil
        },
    )
}

/* answerFirewallDoor serves the handler's response; a handler that answered none redirects an html client and gives any other its own answer. */
func answerFirewallDoor(
    requestEvent *http.KernelRequestEvent,
    response httpcontract.Response,
    targetPath string,
    apiAnswer func() (httpcontract.Response, error),
) {
    if false == internal.IsNilInterface(response) {
        requestEvent.SetResponse(response)

        return
    }

    if true == http.PrefersHtml(requestEvent.Request()) {
        requestEvent.SetResponse(http.RedirectFound(targetPath))

        return
    }

    answer, answerErr := apiAnswer()
    if nil != answerErr {
        requestEvent.Refuse(answerErr)

        return
    }

    requestEvent.SetResponse(answer)
}

/* requestIsCrossSite reads what a browser states about the request's origin: Sec-Fetch-Site when it sent one, where only same-origin and a user-initiated none pass, so a sibling subdomain is refused, else Origin against the request's scheme and host, so a plain http page cannot post to its https twin. A request with neither is no browser's cross-site form and is admitted. */
func requestIsCrossSite(request httpcontract.Request) bool {
    httpRequest := request.HttpRequest()

    fetchSite := strings.ToLower(strings.TrimSpace(httpRequest.Header.Get("Sec-Fetch-Site")))
    if "" != fetchSite {
        return "same-origin" != fetchSite && "none" != fetchSite
    }

    origin := strings.TrimSpace(httpRequest.Header.Get("Origin"))
    if "" == origin {
        return false
    }

    /* "null", an opaque origin, parses to no host and is refused with any foreign one */
    originUrl, parseErr := url.Parse(origin)
    if nil != parseErr {
        return true
    }

    if schemeValue, published := request.Attributes().Get(http.RequestAttributeScheme); true == published {
        if scheme, isText := schemeValue.(string); true == isText && false == strings.EqualFold(originUrl.Scheme, scheme) {
            return true
        }
    }

    return false == strings.EqualFold(originUrl.Host, httpRequest.Host)
}
