package http

import (
	"net"
	nethttp "net/http"
	"net/netip"
	"reflect"
	"strings"

	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	httpcontract "github.com/precision-soft/melody/http/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/precision-soft/melody/session"
	sessioncontract "github.com/precision-soft/melody/session/contract"
)

func wrapControllerWithContainer(
	controller any,
) httpcontract.Handler {
	controllerValue := reflect.ValueOf(controller)
	controllerType := controllerValue.Type()

	if reflect.Func != controllerType.Kind() {
		exception.Panic(
			exception.NewError(
				"controller must be a function",
				exceptioncontract.Context{"type": controllerType.Kind().String()},
				nil,
			),
		)
	}

	if controllerType.NumIn() < 1 {
		exception.Panic(
			exception.NewError(
				"controller must have at least one argument",
				exceptioncontract.Context{
					"expected": "(*Request)",
				},
				nil,
			),
		)
	}

	firstParamType := controllerType.In(0)
	if firstParamType != reflect.TypeOf(&Request{}) {
		exception.Panic(
			exception.NewError(
				"first controller argument must be a request",
				exceptioncontract.Context{
					"type":     controllerType.Kind().String(),
					"expected": "*Request",
				},
				nil,
			),
		)
	}

	if 2 != controllerType.NumOut() {
		exception.Panic(
			exception.NewError(
				"controller must return response",
				exceptioncontract.Context{
					"expected": "(*Response, error)",
				},
				nil,
			),
		)
	}

	if controllerType.Out(0) != reflect.TypeOf(&Response{}) {
		exception.Panic(
			exception.NewError(
				"controller must return response as first result",
				exceptioncontract.Context{
					"controllerType": controllerType.String(),
					"expected":       "*Response",
				},
				nil,
			),
		)
	}

	errorInterfaceType := reflect.TypeOf((*error)(nil)).Elem()
	if controllerType.Out(1) != errorInterfaceType {
		exception.Panic(
			exception.NewError(
				"controller must return error as second result",
				exceptioncontract.Context{
					"controllerType": controllerType.String(),
				},
				nil,
			),
		)
	}

	return func(
		runtimeInstance runtimecontract.Runtime,
		writer nethttp.ResponseWriter,
		request httpcontract.Request,
	) (httpcontract.Response, error) {
		if nil == runtimeInstance {
			return nil, exception.NewError(
				"runtime instance is nil in controller handler",
				nil,
				nil,
			)
		}

		arguments := make([]reflect.Value, controllerType.NumIn())
		arguments[0] = reflect.ValueOf(request)

		for index := 1; index < controllerType.NumIn(); index++ {
			paramType := controllerType.In(index)

			dependency, err := runtimeInstance.Scope().GetByType(paramType)
			if nil != err {
				return nil, err
			}

			arguments[index] = reflect.ValueOf(dependency)
		}

		results := controllerValue.Call(arguments)

		responseValue := results[0]
		errorInterface := results[1].Interface()

		var response *Response
		if false == responseValue.IsNil() {
			response = responseValue.Interface().(*Response)
		}

		var err error
		if nil != errorInterface {
			err = errorInterface.(error)
		}

		if nil == response {
			return nil, err
		}

		return response, err
	}
}

func wrapWithMiddlewares(handler httpcontract.Handler, middlewares []httpcontract.Middleware) httpcontract.Handler {
	wrapped := handler
	for index := len(middlewares) - 1; 0 <= index; index-- {
		wrapped = middlewares[index](wrapped)
	}

	return wrapped
}

func splitPath(value string) []string {
	trimmedPath := strings.TrimSpace(value)
	if "" == trimmedPath {
		return []string{""}
	}

	if false == strings.HasPrefix(trimmedPath, "/") {
		trimmedPath = "/" + trimmedPath
	}

	if 1 < len(trimmedPath) {
		trimmedPath = strings.TrimRight(trimmedPath, "/")
		if "" == trimmedPath {
			trimmedPath = "/"
		}
	}

	return strings.Split(trimmedPath, "/")
}

func writeResponse(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	writer nethttp.ResponseWriter,
	response httpcontract.Response,
	sessionManager sessioncontract.Manager,
	sessionInstance sessioncontract.Session,
	forwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy,
	sessionCookiePolicy httpcontract.SessionCookiePolicy,
) {
	if nil == response {
		writer.WriteHeader(nethttp.StatusNoContent)

		return
	}

	if nil != sessionManager && nil != sessionInstance {
		if true == sessionInstance.IsCleared() {
			err := sessionManager.DeleteSession(sessionInstance.Id())
			if nil != err {
				exception.Panic(
					exception.NewError("failed to delete session", nil, err),
				)
			}

			cookiePath := sessionCookiePolicy.Path
			if "" == cookiePath {
				cookiePath = "/"
			}

			cookie := &nethttp.Cookie{
				Name:     session.SessionCookieName,
				Value:    "",
				Path:     cookiePath,
				Domain:   sessionCookiePolicy.Domain,
				HttpOnly: true,
				SameSite: sessionCookiePolicy.SameSite,
				Secure:   "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy),
				MaxAge:   -1,
			}

			SetCookie(response, cookie)
		} else if true == sessionInstance.IsModified() {
			err := sessionManager.SaveSession(sessionInstance)
			if nil != err {
				exception.Panic(
					exception.NewError("failed to save session", nil, err),
				)
			}

			cookiePath := sessionCookiePolicy.Path
			if "" == cookiePath {
				cookiePath = "/"
			}

			cookie := &nethttp.Cookie{
				Name:     session.SessionCookieName,
				Value:    sessionInstance.Id(),
				Path:     cookiePath,
				Domain:   sessionCookiePolicy.Domain,
				HttpOnly: true,
				SameSite: sessionCookiePolicy.SameSite,
				Secure:   "https" == detectSchemeWithForwardedHeadersPolicy(request.HttpRequest(), forwardedHeadersPolicy),
			}

			SetCookie(response, cookie)
		}
	}

	err := WriteToHttpResponseWriter(runtimeInstance, request, writer, response)
	if nil != err {
		exception.Panic(
			exception.NewError("failed to write response", nil, err),
		)
	}
}

func detectScheme(request *nethttp.Request) string {
	return detectSchemeWithForwardedHeadersPolicy(
		request,
		httpcontract.ForwardedHeadersPolicy{
			TrustForwardedHeaders: false,
			TrustedProxyList:      nil,
		},
	)
}

func detectSchemeWithForwardedHeadersPolicy(request *nethttp.Request, policy httpcontract.ForwardedHeadersPolicy) string {
	if nil == request {
		return "http"
	}

	if nil != request.TLS {
		return "https"
	}

	if false == policy.TrustForwardedHeaders {
		return "http"
	}

	if 0 == len(policy.TrustedProxyList) {
		return "http"
	}

	if false == isRequestFromTrustedProxy(request, policy.TrustedProxyList) {
		return "http"
	}

	forwardedProto := request.Header.Get("X-Forwarded-Proto")
	if "" != forwardedProto {
		return strings.ToLower(forwardedProto)
	}

	return "http"
}

func isRequestFromTrustedProxy(request *nethttp.Request, trustedProxyList []string) bool {
	if nil == request {
		return false
	}

	remoteAddressString := strings.TrimSpace(request.RemoteAddr)
	if "" == remoteAddressString {
		return false
	}

	remoteHostString := remoteAddressString
	hostFromSplit, _, splitErr := net.SplitHostPort(remoteAddressString)
	if nil == splitErr && "" != strings.TrimSpace(hostFromSplit) {
		remoteHostString = hostFromSplit
	}

	remoteAddress, remoteAddressErr := netip.ParseAddr(remoteHostString)
	if nil != remoteAddressErr {
		return false
	}

	for _, trustedProxyString := range trustedProxyList {
		trimmedTrustedProxyString := strings.TrimSpace(trustedProxyString)
		if "" == trimmedTrustedProxyString {
			continue
		}

		trustedPrefix, trustedPrefixErr := netip.ParsePrefix(trimmedTrustedProxyString)
		if nil == trustedPrefixErr {
			if true == trustedPrefix.Contains(remoteAddress) {
				return true
			}

			continue
		}

		trustedAddress, trustedAddressErr := netip.ParseAddr(trimmedTrustedProxyString)
		if nil != trustedAddressErr {
			continue
		}

		if trustedAddress == remoteAddress {
			return true
		}
	}

	return false
}

func matchesMethod(methods []string, method string) bool {
	if 0 == len(methods) {
		return true
	}

	for _, allowedMethod := range methods {
		if allowedMethod == method {
			return true
		}
	}

	return false
}

func matchesHost(expectedHost string, actualHost string) bool {
	if "" == expectedHost {
		return true
	}

	return expectedHost == actualHost
}

func matchesScheme(schemes []string, scheme string) bool {
	if 0 == len(schemes) {
		return true
	}

	for _, allowedScheme := range schemes {
		if strings.EqualFold(allowedScheme, scheme) {
			return true
		}
	}

	return false
}

func matchPath(
	routeDefinition route,
	pathSegments []string,
) (map[string]string, bool) {
	patternSegments := routeDefinition.parts
	params := make(map[string]string)

	pathIndex := 0
	patternIndex := 0

	for patternIndex < len(patternSegments) {
		routePart := patternSegments[patternIndex]
		isLastPattern := patternIndex == len(patternSegments)-1

		if true == strings.HasPrefix(routePart, "*") {
			wildcardName := strings.TrimPrefix(routePart, "*")
			isCatchAll := false
			if true == strings.HasSuffix(wildcardName, "...") {
				isCatchAll = true
				wildcardName = strings.TrimSuffix(wildcardName, "...")
			}

			if true == isLastPattern {
				isCatchAll = true
			}

			if true == isCatchAll {
				rest := ""
				if len(pathSegments) > pathIndex {
					rest = strings.Join(pathSegments[pathIndex:], "/")
				}

				if "" != wildcardName {
					params[wildcardName] = rest
					if RouteAttributeName == wildcardName {
						params[RouteAttributeLocale] = rest
					}
				}

				return params, true
			}

			if pathIndex >= len(pathSegments) {
				return nil, false
			}

			pathPart := pathSegments[pathIndex]
			if "" != wildcardName {
				if regex, exists := routeDefinition.requirements[wildcardName]; true == exists {
					if false == regex.MatchString(pathPart) {
						return nil, false
					}
				}

				params[wildcardName] = pathPart
				if RouteAttributeLocale == wildcardName {
					params[RouteAttributeLocale] = pathPart
				}
			}

			pathIndex++
			patternIndex++

			continue
		}

		if pathIndex >= len(pathSegments) {
			if true == strings.HasPrefix(routePart, ":") {
				paramName := strings.TrimPrefix(routePart, ":")
				if true == strings.HasSuffix(paramName, "?") {
					paramName = strings.TrimSuffix(paramName, "?")

					patternIndex++

					continue
				}
			}

			return nil, false
		}

		pathPart := pathSegments[pathIndex]

		if true == strings.HasPrefix(routePart, ":") {
			paramName := strings.TrimPrefix(routePart, ":")
			if true == strings.HasSuffix(paramName, "?") {
				paramName = strings.TrimSuffix(paramName, "?")
			}

			if regex, exists := routeDefinition.requirements[paramName]; true == exists {
				if false == regex.MatchString(pathPart) {
					return nil, false
				}
			}

			params[paramName] = pathPart

			if RouteAttributeLocale == paramName {
				params[RouteAttributeLocale] = pathPart
			}

			pathIndex++
			patternIndex++

			continue
		}

		if routePart != pathPart {
			return nil, false
		}

		pathIndex++
		patternIndex++
	}

	if pathIndex != len(pathSegments) {
		return nil, false
	}

	return params, true
}
