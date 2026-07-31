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

/* @info a factory that yields nil used to be dropped from the pipeline without a trace — not even the inactive report carried it — so the operator who registered a rate limiter ran every request without one; the build refuses it now, naming the definition. */
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

/* @info the registration gate reads through the interface: a nil function value inside a non-nil interface passed the plain nil comparison and panicked per request instead of at the boot line that declares it */
func TestHttpMiddlewareUse_RefusesATypedNilMiddleware(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    var nilFunction func(next httpcontract.Handler) httpcontract.Handler

    testhelper.AssertPanicsWithError(t, func() {
        middleware.Use(nilFunction)
    }, "middleware is nil in use with priority")
}

/* @info the control: a healthy factory stays in the chain and the pipeline builds */
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
