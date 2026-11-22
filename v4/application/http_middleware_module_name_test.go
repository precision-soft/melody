package application

import (
    "errors"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

var errModuleNameTestFactory = errors.New("the factory could not reach its backend")

func moduleNameTestFailingFactory(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
    return nil, errModuleNameTestFactory
}

/* buildRefusalOf answers the "could not build middleware" link of a refusal's chain */
func buildRefusalOf(t *testing.T, err error) *exception.Error {
    t.Helper()

    for current := err; nil != current; current = errors.Unwrap(current) {
        exceptionErr, isException := current.(*exception.Error)
        if true == isException && "could not build middleware" == exceptionErr.Message() {
            return exceptionErr
        }
    }

    t.Fatalf("expected the build refusal in the chain, got %v", err)

    return nil
}

type failingFactoryModule struct {
    fakeModule
}

func (instance *failingFactoryModule) RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar applicationcontract.HttpMiddlewareRegistrar) {
    registrar.Use(applicationcontract.MiddlewareRegistration{
        Factories: []middlewarepipeline.MiddlewareFactory{moduleNameTestFailingFactory},
    })
}

/* a cli boot runs the module hooks and builds no pipeline, so the build the http boot would run is asked for afterwards, where its refusal can be read rather than exiting the process */
func TestBoot_AModulesFailingMiddlewareFactoryIsRefusedNamingTheModule(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeCli})
    applicationInstance.httpMiddlewares = NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), applicationInstance.configuration), applicationInstance.configuration)

    applicationInstance.RegisterModule(&failingFactoryModule{fakeModule: fakeModule{name: "billing"}})

    applicationInstance.Boot()

    _, buildErr := applicationInstance.httpMiddlewares.buildForInspection(applicationInstance.kernel)

    refusal := buildRefusalOf(t, buildErr)

    if "billing" != refusal.Context()["moduleName"] {
        t.Fatalf("expected the refusal to name the module, got %v", refusal.Context())
    }

    if middlewareName, _ := refusal.Context()["middlewareName"].(string); "" == middlewareName {
        t.Fatalf("expected the refusal to name the middleware, got %v", refusal.Context())
    }

    if false == errors.Is(buildErr, errModuleNameTestFactory) {
        t.Fatalf("expected the factory's error reachable, got %v", buildErr)
    }
}

func TestHttpMiddleware_AnApplicationFactoryIsRefusedNamingTheApplication(t *testing.T) {
    applicationInstance := newEnvironmentKeyBootApplication(t, nil)
    applicationInstance.httpMiddlewares = NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), applicationInstance.configuration), applicationInstance.configuration)

    applicationInstance.RegisterHttpMiddlewareFactories([]middlewarepipeline.MiddlewareFactory{moduleNameTestFailingFactory})

    _, buildErr := applicationInstance.httpMiddlewares.buildForInspection(newTestKernel())

    if internal.EnvironmentKeyOwnerApplication != buildRefusalOf(t, buildErr).Context()["moduleName"] {
        t.Fatalf("expected the refusal to name the application, got %v", buildErr)
    }
}

func TestHttpMiddleware_ARegistrationRefusalNamesTheModuleOnlyWhenOneRegisters(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    refusalContext := func(registrar applicationcontract.HttpMiddlewareRegistrar) map[string]any {
        var recovered any
        func() {
            defer func() { recovered = recover() }()

            registrar.Use(applicationcontract.MiddlewareRegistration{Factories: []middlewarepipeline.MiddlewareFactory{nil}})
        }()

        var exceptionErr *exception.Error
        recoveredErr, isError := recovered.(error)
        if false == isError || false == errors.As(recoveredErr, &exceptionErr) {
            t.Fatalf("expected the nil factory refused, got %v", recovered)
        }

        return exceptionErr.Context()
    }

    if "billing" != refusalContext(middleware.forModule("billing"))["moduleName"] {
        t.Fatalf("expected the module named in the registration refusal")
    }

    if _, present := refusalContext(middleware)["moduleName"]; true == present {
        t.Fatalf("expected no module key when no module registers")
    }
}

func TestHttpMiddleware_AFactoryYieldingNilIsRefusedNamingTheModule(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.forModule("billing").Use(applicationcontract.MiddlewareRegistration{Factories: []middlewarepipeline.MiddlewareFactory{func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
        return nil, nil
    }}})

    _, buildErr := middleware.buildForInspection(newTestKernel())

    var nilRefusal *exception.Error
    for current := buildErr; nil != current; current = errors.Unwrap(current) {
        exceptionErr, isException := current.(*exception.Error)
        if true == isException && "middleware factory returned nil" == exceptionErr.Message() {
            nilRefusal = exceptionErr
        }
    }

    if nil == nilRefusal || "billing" != nilRefusal.Context()["moduleName"] || "billing" != buildRefusalOf(t, buildErr).Context()["moduleName"] {
        t.Fatalf("expected both refusals to name the module, got %v", buildErr)
    }
}
