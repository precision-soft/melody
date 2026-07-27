package config

import (
    nethttp "net/http"
    "strconv"

    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodyclockcontract "github.com/precision-soft/melody/v2/clock/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func (instance *Module) RegisterHttpMiddlewares(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.HttpMiddlewareRegistrar) {
    registrar.Use(NewTimingMiddleware(kernelInstance.Clock()))
}

/* NewTimingMiddleware measures how long a request took and reports it in a header.

The clock is injected rather than read from the wall, which is what makes the header assertable: a frozen clock advanced by the handler under it lets a test state the exact duration the header must carry, and no test can state that against time.Now. */
func NewTimingMiddleware(clockInstance melodyclockcontract.Clock) melodyhttpcontract.Middleware {
    return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
        return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            startedAt := clockInstance.Now()

            response, err := next(runtimeInstance, writer, request)
            if nil != err {
                return response, err
            }

            duration := clockInstance.Now().Sub(startedAt).Milliseconds()
            if nil != response {
                response.Headers().Set("X-Example-Duration-Ms", strconv.FormatInt(duration, 10))
            }

            return response, nil
        }
    }
}

var _ melodyapplicationcontract.HttpMiddlewareModule = (*Module)(nil)
