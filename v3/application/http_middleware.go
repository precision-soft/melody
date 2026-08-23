package application

import (
    "fmt"
    "reflect"
    "runtime"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/http/middleware"
    middlewarepipeline "github.com/precision-soft/melody/v3/http/middleware/pipeline"
    "github.com/precision-soft/melody/v3/http/static"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

const (
    MiddlewareGroupHttp = "http"

    /* the pipeline sorts ascending and the first entry becomes the outermost wrapper, so a priority BELOW the default puts the static file server outermost, which is where it is. A request for a file that exists is therefore answered before anything registered through Use observes it: the file server never calls the rest of the chain, so the rate limiter, the compressor and the access log are all skipped for exactly the requests that read files off disk. That is the deliberate trade — the alternative is running the whole application chain for every asset — and an application that needs its own middleware to see static requests registers that middleware below this priority rather than above it. */
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

        if true == internal.IsNilInterface(middlewareInstance) {
            exception.Panic(exception.NewError("middleware is nil in use with priority", nil, nil))
        }

        instance.customIndex = instance.customIndex + 1
        name := fmt.Sprintf("middleware.%d.%d", instance.customIndex, priority)

        definition := middlewarepipeline.NewHttpMiddlewareDefinition(
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
        )
        definition.SetFunctionName(registeredFunctionName(middlewareInstance))

        instance.definitions = append(instance.definitions, definition)
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

        definition := middlewarepipeline.NewHttpMiddlewareDefinition(
            name,
            priority,
            make([]string, 0),
            make([]string, 0),
            []string{MiddlewareGroupHttp},
            make([]string, 0),
            func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                /* a factory that yields nil is refused at the build instead of being dropped from the chain: the pipeline skips a nil middleware without recording it anywhere, so the operator who registered a rate limiter here would run every request without it and be told nothing. The typed-nil yield is the same absence one assertion later — it would join the chain and panic per request. */
                middlewareInstance := factoryInstance(kernelInstance)
                if true == internal.IsNilInterface(middlewareInstance) {
                    return nil, exception.NewError(
                        "middleware factory returned nil",
                        exceptioncontract.Context{
                            "middlewareName": name,
                        },
                        nil,
                    )
                }

                return middlewareInstance, nil
            },
            false,
            true,
        )
        definition.SetFunctionName(registeredFunctionName(factoryInstance))

        instance.definitions = append(instance.definitions, definition)
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

/* describe answers what all() would build without building it: no factory runs and the last build report — the serving process's record — is left alone, which is what lets a console command list the pipeline of a process that will never serve */
func (instance *HttpMiddleware) describe(kernelInstance kernelcontract.Kernel) ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
    builder := middlewarepipeline.NewBuilder(instance.defaultDefinitions(kernelInstance)...)
    builder.Add(instance.definitions...)

    return builder.Describe(kernelInstance.Environment(), MiddlewareGroupHttp)
}

/* buildForInspection runs the real build for an explicit --build request, answering the error instead of panicking and leaving the last build report to the serving path */
func (instance *HttpMiddleware) buildForInspection(kernelInstance kernelcontract.Kernel) ([]httpcontract.Middleware, error) {
    builder := middlewarepipeline.NewBuilder(instance.defaultDefinitions(kernelInstance)...)
    builder.Add(instance.definitions...)

    middlewares, _, buildErr := builder.Build(kernelInstance, MiddlewareGroupHttp)

    return middlewares, buildErr
}

/* registeredFunctionName resolves the name of the function value a registration hands in, at registration time — the one moment the value is certainly present and running nothing */
func registeredFunctionName(value any) string {
    reflectedValue := reflect.ValueOf(value)
    if reflect.Func != reflectedValue.Kind() {
        return ""
    }

    pointer := reflectedValue.Pointer()
    if 0 == pointer {
        return ""
    }

    function := runtime.FuncForPC(pointer)
    if nil == function {
        return ""
    }

    return function.Name()
}

func (instance *HttpMiddleware) defaultDefinitions(kernelInstance kernelcontract.Kernel) []*middlewarepipeline.HttpMiddlewareDefinition {
    definitions := make([]*middlewarepipeline.HttpMiddlewareDefinition, 0, 5)

    staticDefinition := middlewarepipeline.NewHttpMiddlewareDefinition(
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
    )
    staticDefinition.SetFunctionName(registeredFunctionName(middleware.StaticMiddleware))

    definitions = append(definitions, staticDefinition)

    return definitions
}

var _ applicationcontract.HttpMiddlewareRegistrar = (*HttpMiddleware)(nil)
