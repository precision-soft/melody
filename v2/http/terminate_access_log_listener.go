package http

import (
	"time"

	eventcontract "github.com/precision-soft/melody/v2/event/contract"
	kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
	"github.com/precision-soft/melody/v2/logging"
	loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const (
	KernelTerminateAccessLogListenerPriority = -100
)

func RegisterKernelTerminateAccessLogListener(eventDispatcher eventcontract.EventDispatcher) {
	eventDispatcher.AddListener(
		kernelcontract.EventKernelTerminate,
		func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
			terminateEvent, ok := eventValue.Payload().(*KernelTerminateEvent)
			if false == ok {
				return nil
			}

			if nil == terminateEvent {
				return nil
			}

			if nil == terminateEvent.Request() || nil == terminateEvent.Request().RequestContext() {
				return nil
			}

			if nil == terminateEvent.Runtime() {
				return nil
			}

			loggerInstance := logging.LoggerMustFromRuntime(terminateEvent.Runtime())
			if nil == loggerInstance {
				return nil
			}

			requestContextInstance := terminateEvent.Request().RequestContext()

			requestId := requestContextInstance.RequestId()
			method := ""
			path := ""

			if nil != terminateEvent.Request().HttpRequest() {
				method = terminateEvent.Request().HttpRequest().Method
				if nil != terminateEvent.Request().HttpRequest().URL {
					path = terminateEvent.Request().HttpRequest().URL.Path
				}
			}

			statusCode := 0
			if nil != terminateEvent.Response() {
				statusCode = terminateEvent.Response().StatusCode()
			}

			duration := time.Duration(0)

			if nil != terminateEvent.Request().RequestContext() {
				duration = time.Since(terminateEvent.Request().RequestContext().StartedAt())
			}

			routeName := ""
			routePattern := ""

			if nil != terminateEvent.Request() {
				routeName = terminateEvent.Request().RouteName()
				routePattern = terminateEvent.Request().RoutePattern()
			}

			scheme := ""
			host := ""
			queryString := ""
			remoteAddr := ""
			userAgent := ""
			referer := ""

			if nil != terminateEvent.Request().HttpRequest() {
				scheme = detectScheme(terminateEvent.Request().HttpRequest())
				host = terminateEvent.Request().HttpRequest().Host

				if nil != terminateEvent.Request().HttpRequest().URL {
					queryString = terminateEvent.Request().HttpRequest().URL.RawQuery
				}

				remoteAddr = terminateEvent.Request().HttpRequest().RemoteAddr
				userAgent = terminateEvent.Request().HttpRequest().UserAgent()
				referer = terminateEvent.Request().HttpRequest().Referer()
			}

			loggerInstance.Info(
				"request completed",
				loggingcontract.Context{
					"requestId":    requestId,
					"method":       method,
					"path":         path,
					"query":        queryString,
					"scheme":       scheme,
					"host":         host,
					"remoteAddr":   remoteAddr,
					"userAgent":    userAgent,
					"referer":      referer,
					"routeName":    routeName,
					"routePattern": routePattern,
					"statusCode":   statusCode,
					"durationMs":   duration.Milliseconds(),
				},
			)

			return nil
		},
		KernelTerminateAccessLogListenerPriority,
	)
}
