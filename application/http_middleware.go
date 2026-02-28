package application

import (
    "fmt"

    applicationcontract "github.com/precision-soft/melody/application/contract"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/http/middleware"
    middlewarepipeline "github.com/precision-soft/melody/http/middleware/pipeline"
    "github.com/precision-soft/melody/http/static"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

const (
    MiddlewareGroupHttp = "http"

    MiddlewarePriorityStatic = -1000
    MiddlewareNameStatic     = "static"

    MiddlewarePriorityDefault = 0
)

type MiddlewareFactory func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware

type HttpMiddleware struct {
    staticOptions   *static.Options
    definitions     []*middlewarepipeline.HttpMiddlewareDefinition
    customIndex     int
    lastBuildReport *middlewarepipeline.MiddlewareBuildReport
}

type httpMiddlewareChain = []httpcontract.Middleware

func NewHttpMiddleware(
    staticOptions *static.Options,
    configuration configcontract.Configuration,
) *HttpMiddleware {
    return &HttpMiddleware{
        definitions:   make([]*middlewarepipeline.HttpMiddlewareDefinition, 0),
        staticOptions: staticOptions,
        customIndex:   0,
        lastBuildReport: middlewarepipeline.NewMiddlewareBuildReport(
            "",
            configuration.Kernel().Env(),
            make([]string, 0),
            make([]*middlewarepipeline.InactiveMiddleware, 0),
            nil,
            false,
        ),
    }
}

func (instance *HttpMiddleware) Use(middlewares ...httpcontract.Middleware) {
    instance.UseWithPriority(MiddlewarePriorityDefault, middlewares...)
}

func (instance *HttpMiddleware) UseWithPriority(priority int, middlewares ...httpcontract.Middleware) {
    if 0 == len(middlewares) {
        return
    }

    for _, currentMiddleware := range middlewares {
        middlewareInstance := currentMiddleware

        if nil == middlewareInstance {
            exception.Panic(exception.NewError("middleware is nil in use with priority", nil, nil))
        }

        instance.customIndex = instance.customIndex + 1
        name := fmt.Sprintf("middleware.%d.%d", instance.customIndex, priority)

        instance.definitions = append(
            instance.definitions,
            middlewarepipeline.NewHttpMiddlewareDefinition(
                name,
                priority,
                make([]string, 0),
                make([]string, 0),
                []string{MiddlewareGroupHttp},
                make([]string, 0),
                func(_ kernelcontract.Kernel) (httpcontract.Middleware, error) {
                    return middlewareInstance, nil
                },
                false,
                true,
            ),
        )
    }
}

func (instance *HttpMiddleware) UseFactories(factories ...MiddlewareFactory) {
    instance.UseFactoriesWithPriority(MiddlewarePriorityDefault, factories...)
}

func (instance *HttpMiddleware) UseFactoriesWithPriority(priority int, factories ...MiddlewareFactory) {
    if 0 == len(factories) {
        return
    }

    for _, currentFactory := range factories {
        factoryInstance := currentFactory

        if nil == factoryInstance {
            exception.Panic(exception.NewError("middleware factory instance is nil in use with priority", nil, nil))
        }

        instance.customIndex = instance.customIndex + 1
        name := fmt.Sprintf("factory.%d.%d", instance.customIndex, priority)

        instance.definitions = append(
            instance.definitions,
            middlewarepipeline.NewHttpMiddlewareDefinition(
                name,
                priority,
                make([]string, 0),
                make([]string, 0),
                []string{MiddlewareGroupHttp},
                make([]string, 0),
                func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                    return factoryInstance(kernelInstance), nil
                },
                false,
                true,
            ),
        )
    }
}

func (instance *HttpMiddleware) LastBuildReport() *middlewarepipeline.MiddlewareBuildReport {
    return instance.lastBuildReport
}

func (instance *HttpMiddleware) all(kernelInstance kernelcontract.Kernel) httpMiddlewareChain {
    builder := middlewarepipeline.NewBuilder(instance.defaultDefinitions(kernelInstance)...)
    builder.Add(instance.definitions...)

    middlewares, report, buildErr := builder.Build(kernelInstance, MiddlewareGroupHttp)
    if nil != buildErr {
        exception.Panic(
            exception.NewError("failed to build middleware pipeline", nil, buildErr),
        )
    }

    instance.lastBuildReport = report

    return middlewares
}

func (instance *HttpMiddleware) defaultDefinitions(kernelInstance kernelcontract.Kernel) []*middlewarepipeline.HttpMiddlewareDefinition {
    definitions := make([]*middlewarepipeline.HttpMiddlewareDefinition, 0, 5)

    definitions = append(
        definitions,
        middlewarepipeline.NewHttpMiddlewareDefinition(
            MiddlewareNameStatic,
            MiddlewarePriorityStatic,
            make([]string, 0),
            make([]string, 0),
            []string{MiddlewareGroupHttp},
            make([]string, 0),
            func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                return middleware.StaticMiddleware(instance.staticOptions), nil
            },
            false,
            false,
        ),
    )

    return definitions
}

var _ applicationcontract.HttpMiddlewareRegistrar = (*HttpMiddleware)(nil)
