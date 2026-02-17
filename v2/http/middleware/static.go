package middleware

import (
	nethttp "net/http"

	"github.com/precision-soft/melody/v2/http"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	"github.com/precision-soft/melody/v2/http/static"
	"github.com/precision-soft/melody/v2/logging"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func StaticMiddleware(
	options *static.Options,
) httpcontract.Middleware {
	staticServer := static.NewFileServer(options)

	return func(next httpcontract.Handler) httpcontract.Handler {
		return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			logger := logging.LoggerMustFromRuntime(runtimeInstance)

			statusCode, headers, bodyReader, ok := staticServer.ServeReader(request, logger)
			if true == ok {
				response := http.EmptyResponse(statusCode)

				if nil != bodyReader {
					response.SetBodyReader(bodyReader)
				}

				if nil != headers {
					for key, values := range headers {
						for _, value := range values {
							response.Headers().Add(key, value)
						}
					}
				}

				return response, nil
			}

			return next(runtimeInstance, writer, request)
		}
	}
}
