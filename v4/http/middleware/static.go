package middleware

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/http/static"
    "github.com/precision-soft/melody/v4/logging"
)

func StaticMiddleware(config static.Config) httpcontract.Middleware {
    staticServer := static.NewFileServer(config)

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            logger := logging.LoggerMustFromRuntime(runtimeInstance)

            statusCode, headers, bodyReader, ok := staticServer.ServeReader(request, logger)
            if true == ok {
                response := http.EmptyResponse(statusCode)

                if nil != bodyReader {
                    response.SetBodyReader(bodyReader)
                }

                if nil != headers {
                    for key, values := range headers {
                        for index, value := range values {
                            if 0 == index {
                                response.Headers().Set(key, value)
                                continue
                            }
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
