package application

import (
    "strings"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

func newHttpMiddlewareUnderTest(t *testing.T) *HttpMiddleware {
    t.Helper()

    configuration := newCollisionTestConfiguration(t)

    return NewHttpMiddleware(
        newStaticFileServerOptions(testhelper.NewEmbeddedStaticFs(), configuration),
        configuration,
    )
}

func TestHttpMiddlewareAll_RefusesAFactoryThatYieldsNil(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.UseFactories(func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        return nil
    })

    testhelper.AssertPanicsWithError(t, func() {
        middleware.all(newTestKernel())
    }, "failed to build middleware pipeline")
}

func TestHttpMiddlewareAll_RefusesAFactoryThatYieldsATypedNil(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.UseFactories(func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        var typedNil httpcontract.Middleware
        return typedNil
    })

    testhelper.AssertPanicsWithError(t, func() {
        middleware.all(newTestKernel())
    }, "failed to build middleware pipeline")
}

func TestHttpMiddlewareUse_RefusesATypedNilMiddleware(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    var nilFunction func(next httpcontract.Handler) httpcontract.Handler

    testhelper.AssertPanicsWithError(t, func() {
        middleware.Use(nilFunction)
    }, "middleware is nil in use with priority")
}

func TestHttpMiddlewareAll_BuildsWithAHealthyFactory(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.UseFactories(func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        return func(next httpcontract.Handler) httpcontract.Handler {
            return next
        }
    })

    middlewares := middleware.all(newTestKernel())

    if 2 > len(middlewares) {
        t.Fatalf("expected the static middleware and the factory's middleware in the chain, got %d", len(middlewares))
    }

    if false == strings.Contains(strings.Join(middleware.LastBuildReport().SelectedNames(), ","), "factory.1.0") {
        t.Fatalf("expected the factory's definition among the selected names, got %v", middleware.LastBuildReport().SelectedNames())
    }
}

/* describing the pipeline runs no factory and leaves the serving path's report alone, while still naming the function captured at registration — the console listing must have no side effect in a process that will never serve */
func TestHttpMiddlewareDescribe_RunsNoFactoryAndNamesTheRegisteredFunction(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    factoryRuns := 0

    applicationInstance.RegisterHttpMiddlewareFactories(func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        factoryRuns = factoryRuns + 1

        return func(next httpcontract.Handler) httpcontract.Handler {
            return next
        }
    })

    applicationInstance.Boot()

    reportBefore := applicationInstance.httpMiddlewares.LastBuildReport()

    descriptions, _, describeErr := applicationInstance.httpMiddlewares.describe(applicationInstance.kernel)
    if nil != describeErr {
        t.Fatalf("expected no error, got %v", describeErr)
    }

    if 0 != factoryRuns {
        t.Fatalf("expected the description to run no factory, ran %d times", factoryRuns)
    }

    if reportBefore != applicationInstance.httpMiddlewares.LastBuildReport() {
        t.Fatal("expected the description to leave the last build report alone")
    }

    if 2 != len(descriptions) {
        t.Fatalf("expected the static middleware and the factory described, got %d", len(descriptions))
    }

    factoryFunctionNamed := false
    for _, description := range descriptions {
        if "" != description.FunctionName && true == strings.Contains(description.FunctionName, "TestHttpMiddlewareDescribe_RunsNoFactoryAndNamesTheRegisteredFunction") {
            factoryFunctionNamed = true
        }
    }

    if false == factoryFunctionNamed {
        t.Fatalf("expected the registered factory's function name on its description, got %+v", descriptions)
    }
}
