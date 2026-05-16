package config

import (
    nethttp "net/http"
    "strconv"
    "time"

    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func (instance *Module) RegisterHttpMiddlewares(kernelInstance melodykernelcontract.Kernel, registrar melodyapplicationcontract.HttpMiddlewareRegistrar) {
    registrar.Use(NewTimingMiddleware())
}

func NewTimingMiddleware() melodyhttpcontract.Middleware {
    return func(next melodyhttpcontract.Handler) melodyhttpcontract.Handler {
        return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            startedAt := time.Now()

            response, err := next(runtimeInstance, writer, request)
            if nil != err {
                return response, err
            }

            duration := time.Since(startedAt).Milliseconds()
            if nil != response {
                response.Headers().Set("X-Example-Duration-Ms", strconv.FormatInt(duration, 10))
            }

            return response, nil
        }
    }
}

var _ melodyapplicationcontract.HttpMiddlewareModule = (*Module)(nil)
