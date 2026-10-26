package application

import (
    "fmt"
    "reflect"
    "runtime"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/http/middleware"
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "github.com/precision-soft/melody/v4/http/static"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

const (
    MiddlewareGroupHttp = "http"

    /* MiddlewarePriorityStatic puts the static file server outermost, below the default priority: a request for a file that exists is answered before anything registered through Use observes it, so the rate limiter, the compressor and any access log written as a middleware are skipped for it, while the framework's own access log, a kernel.terminate listener, still records it. A middleware that must see static requests registers below this priority. */
    MiddlewarePriorityStatic = -1000
    MiddlewareNameStatic     = "static"

    /* MiddlewarePriorityDefault is the priority a MiddlewareRegistration that leaves Priority out registers at. */
    MiddlewarePriorityDefault = 0
)

type MiddlewareFactory = applicationcontract.MiddlewareFactory

type HttpMiddleware struct {
    staticConfig    static.Config
    definitions     []middlewarepipeline.HttpMiddlewareDefinition
    customIndex     int
    lastBuildReport *middlewarepipeline.MiddlewareBuildReport
}

type httpMiddlewareChain = []httpcontract.Middleware

func NewHttpMiddleware(
    staticConfig static.Config,
    configuration configcontract.Configuration,
) *HttpMiddleware {
    return &HttpMiddleware{
        definitions:   make([]middlewarepipeline.HttpMiddlewareDefinition, 0),
        staticConfig:  staticConfig,
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

func (instance *HttpMiddleware) Use(registration applicationcontract.MiddlewareRegistration) {
    instance.useMiddlewares(registration.Priority, registration.Middlewares)
    instance.useFactories(registration.Priority, registration.Factories)
}

func (instance *HttpMiddleware) useMiddlewares(priority int, middlewares []httpcontract.Middleware) {
    for middlewareIndex, currentMiddleware := range middlewares {
        middlewareInstance := currentMiddleware

        if true == internal.IsNilInterface(middlewareInstance) {
            exception.Panic(exception.NewError("middleware is nil in a middleware registration", exceptioncontract.Context{"field": "MiddlewareRegistration.Middlewares", "index": middlewareIndex}, nil))
        }

        instance.customIndex = instance.customIndex + 1
        name := fmt.Sprintf("middleware.%d.%d", instance.customIndex, priority)

        definition := middlewarepipeline.HttpMiddlewareDefinition{
            Name:     name,
            Priority: priority,
            Groups:   []string{MiddlewareGroupHttp},
            Factory: func(_ kernelcontract.Kernel) (httpcontract.Middleware, error) {
                return middlewareInstance, nil
            },
            AllowDuplicates: true,
            FunctionName:    registeredFunctionName(middlewareInstance),
        }

        instance.definitions = append(instance.definitions, definition)
    }
}

func (instance *HttpMiddleware) useFactories(priority int, factories []MiddlewareFactory) {
    for factoryIndex, currentFactory := range factories {
        factoryInstance := currentFactory

        if nil == factoryInstance {
            exception.Panic(exception.NewError("middleware factory is nil in a middleware registration", exceptioncontract.Context{"field": "MiddlewareRegistration.Factories", "index": factoryIndex}, nil))
        }

        instance.customIndex = instance.customIndex + 1
        name := fmt.Sprintf("factory.%d.%d", instance.customIndex, priority)

        definition := middlewarepipeline.HttpMiddlewareDefinition{
            Name:     name,
            Priority: priority,
            Groups:   []string{MiddlewareGroupHttp},
            Factory: func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                /* a factory that yields nil, or a typed nil, is refused at the build: an application factory has no way to declare a middleware inactive, so a nil is a wiring mistake that would otherwise run every request without it. A conditional middleware is left unregistered at the composition root. */
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
            AllowDuplicates: true,
            FunctionName:    registeredFunctionName(factoryInstance),
        }

        instance.definitions = append(instance.definitions, definition)
    }
}

func (instance *HttpMiddleware) LastBuildReport() *middlewarepipeline.MiddlewareBuildReport {
    return instance.lastBuildReport
}

func (instance *HttpMiddleware) all(kernelInstance kernelcontract.Kernel) httpMiddlewareChain {
    builder := middlewarepipeline.NewBuilder(instance.defaultDefinitions(kernelInstance))
    builder.Add(instance.definitions)

    middlewares, report, buildErr := builder.Build(kernelInstance, MiddlewareGroupHttp)
    if nil != buildErr {
        exception.Panic(
            exception.NewError("failed to build middleware pipeline", nil, buildErr),
        )
    }

    instance.lastBuildReport = report

    return middlewares
}

/* describe answers what all() would build without running any factory or touching the last build report, so a console command can list the pipeline of a process that never serves */
func (instance *HttpMiddleware) describe(kernelInstance kernelcontract.Kernel) ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
    builder := middlewarepipeline.NewBuilder(instance.defaultDefinitions(kernelInstance))
    builder.Add(instance.definitions)

    return builder.Describe(kernelInstance.Environment(), MiddlewareGroupHttp)
}

/* buildForInspection runs the real build for an explicit --build request, answering the error instead of panicking and leaving the last build report to the serving path */
func (instance *HttpMiddleware) buildForInspection(kernelInstance kernelcontract.Kernel) ([]httpcontract.Middleware, error) {
    builder := middlewarepipeline.NewBuilder(instance.defaultDefinitions(kernelInstance))
    builder.Add(instance.definitions)

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

func (instance *HttpMiddleware) defaultDefinitions(kernelInstance kernelcontract.Kernel) []middlewarepipeline.HttpMiddlewareDefinition {
    definitions := make([]middlewarepipeline.HttpMiddlewareDefinition, 0, 5)

    staticDefinition := middlewarepipeline.HttpMiddlewareDefinition{
        Name:     MiddlewareNameStatic,
        Priority: MiddlewarePriorityStatic,
        Groups:   []string{MiddlewareGroupHttp},
        Factory: func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
            return middleware.StaticMiddleware(instance.staticConfig), nil
        },
        FunctionName: registeredFunctionName(middleware.StaticMiddleware),
    }

    definitions = append(definitions, staticDefinition)

    return definitions
}

var _ applicationcontract.HttpMiddlewareRegistrar = (*HttpMiddleware)(nil)
