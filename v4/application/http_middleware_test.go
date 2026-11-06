package application

import (
    "context"
    "errors"
    "strings"
    "testing"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

func newHttpMiddlewareUnderTest(t *testing.T) *HttpMiddleware {
    t.Helper()

    configuration := newCollisionTestConfiguration(t)

    return NewHttpMiddleware(
        newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration),
        configuration,
    )
}

func TestHttpMiddlewareAll_RefusesAFactoryThatYieldsNil(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.Use(applicationcontract.MiddlewareRegistration{Factories: []MiddlewareFactory{func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        return nil
    }}})

    testhelper.AssertPanicsWithError(t, func() {
        middleware.all(newTestKernel())
    }, "failed to build middleware pipeline")
}

func TestHttpMiddlewareAll_RefusesAFactoryThatYieldsATypedNil(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.Use(applicationcontract.MiddlewareRegistration{Factories: []MiddlewareFactory{func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        var typedNil httpcontract.Middleware
        return typedNil
    }}})

    testhelper.AssertPanicsWithError(t, func() {
        middleware.all(newTestKernel())
    }, "failed to build middleware pipeline")
}

func TestHttpMiddlewareUse_RefusesATypedNilMiddleware(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    var nilFunction func(next httpcontract.Handler) httpcontract.Handler

    testhelper.AssertPanicsWithError(t, func() {
        middleware.Use(applicationcontract.MiddlewareRegistration{Middlewares: []httpcontract.Middleware{nilFunction}})
    }, "middleware is nil in a middleware registration")
}

func TestHttpMiddlewareAll_BuildsWithAHealthyFactory(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.Use(applicationcontract.MiddlewareRegistration{Factories: []MiddlewareFactory{func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        return func(next httpcontract.Handler) httpcontract.Handler {
            return next
        }
    }}})

    middlewares := middleware.all(newTestKernel())

    if 2 > len(middlewares) {
        t.Fatalf("expected the static middleware and the factory's middleware in the chain, got %d", len(middlewares))
    }

    if false == strings.Contains(strings.Join(middleware.LastBuildReport().SelectedNames(), ","), "factory.1.0") {
        t.Fatalf("expected the factory's definition among the selected names, got %v", middleware.LastBuildReport().SelectedNames())
    }
}

/* describing the pipeline runs no factory and leaves the serving path's report alone while naming the function captured at registration, so the console listing has no side effect in a process that will never serve; the name is captured on the middleware door too, since a middleware handed in as a value has no factory whose name could stand in for it */
func TestHttpMiddlewareDescribe_NamesAMiddlewareRegisteredAsAValue(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.RegisterHttpMiddlewares([]httpcontract.Middleware{namedProbeMiddleware})

    applicationInstance.Boot()

    descriptions, _, describeErr := applicationInstance.httpMiddlewares.describe(applicationInstance.kernel)
    if nil != describeErr {
        t.Fatalf("expected no error, got %v", describeErr)
    }

    valueMiddlewareNamed := false
    for _, description := range descriptions {
        if true == strings.Contains(description.FunctionName, "namedProbeMiddleware") {
            valueMiddlewareNamed = true
        }
    }

    if false == valueMiddlewareNamed {
        t.Fatalf("expected the registered middleware to be named in the description, got %v", descriptions)
    }
}

/* namedProbeMiddleware exists as a declared function precisely so the description has a name to report: an anonymous literal would carry the enclosing test's name and could not tell the two registration doors apart. */
func namedProbeMiddleware(next httpcontract.Handler) httpcontract.Handler {
    return next
}

func TestHttpMiddlewareDescribe_RunsNoFactoryAndNamesTheRegisteredFunction(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    factoryRuns := 0

    applicationInstance.RegisterHttpMiddlewareFactories([]MiddlewareFactory{func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        factoryRuns = factoryRuns + 1

        return func(next httpcontract.Handler) httpcontract.Handler {
            return next
        }
    }})

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

func passThroughMiddleware(next httpcontract.Handler) httpcontract.Handler {
    return next
}

func passThroughFactory(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
    return passThroughMiddleware
}

func TestHttpMiddlewareUse_TheZeroRegistrationInstallsNothing(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.Use(applicationcontract.MiddlewareRegistration{})

    middleware.all(newTestKernel())

    if selectedNames := middleware.LastBuildReport().SelectedNames(); 1 != len(selectedNames) || MiddlewareNameStatic != selectedNames[0] {
        t.Fatalf("expected only the static definition, got %v", selectedNames)
    }
}

func TestHttpMiddlewareUse_InstallsTheMiddlewaresThenTheFactoriesInTheOrderGiven(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    wrapped := make([]string, 0, 4)
    labelled := func(label string) httpcontract.Middleware {
        return func(next httpcontract.Handler) httpcontract.Handler {
            wrapped = append(wrapped, label)

            return next
        }
    }
    labelledFactory := func(label string) MiddlewareFactory {
        return func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
            return labelled(label)
        }
    }

    middleware.Use(applicationcontract.MiddlewareRegistration{
        Middlewares: []httpcontract.Middleware{labelled("first"), labelled("second")},
        Factories:   []MiddlewareFactory{labelledFactory("third"), labelledFactory("fourth")},
    })

    chain := middleware.all(newTestKernel())

    if selectedNames := strings.Join(middleware.LastBuildReport().SelectedNames(), ","); "static,middleware.1.0,middleware.2.0,factory.3.0,factory.4.0" != selectedNames {
        t.Fatalf("expected the middlewares then the factories with consecutive indexes, got %s", selectedNames)
    }

    for _, chainMiddleware := range chain {
        chainMiddleware(nil)
    }

    if "first,second,third,fourth" != strings.Join(wrapped, ",") {
        t.Fatalf("expected the chain to carry the values in the order given, got %v", wrapped)
    }
}

func TestHttpMiddlewareUse_ANilEntryPanicsNamingTheFieldAndTheIndex(t *testing.T) {
    for _, testCase := range []struct {
        name         string
        registration applicationcontract.MiddlewareRegistration
        field        string
    }{
        {name: "middleware", registration: applicationcontract.MiddlewareRegistration{Middlewares: []httpcontract.Middleware{passThroughMiddleware, nil}}, field: "MiddlewareRegistration.Middlewares"},
        {name: "factory", registration: applicationcontract.MiddlewareRegistration{Factories: []MiddlewareFactory{passThroughFactory, nil}}, field: "MiddlewareRegistration.Factories"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            middleware := newHttpMiddlewareUnderTest(t)

            defer func() {
                var refusal *exception.Error
                recoveredErr, isError := recover().(error)
                if false == isError || false == errors.As(recoveredErr, &refusal) {
                    t.Fatalf("expected an error panic, got %v", recoveredErr)
                }

                if testCase.field != refusal.Context()["field"] || 1 != refusal.Context()["index"] {
                    t.Fatalf("expected the refusal to name %s at index 1, got %v", testCase.field, refusal.Context())
                }
            }()

            middleware.Use(testCase.registration)
        })
    }
}

func TestHttpMiddlewareUse_APriorityBelowTheStaticOneWrapsOutsideIt(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.Use(applicationcontract.MiddlewareRegistration{Priority: -2000, Middlewares: []httpcontract.Middleware{passThroughMiddleware}})
    middleware.Use(applicationcontract.MiddlewareRegistration{Middlewares: []httpcontract.Middleware{passThroughMiddleware}})

    middleware.all(newTestKernel())

    if selectedNames := strings.Join(middleware.LastBuildReport().SelectedNames(), ","); "middleware.1.-2000,static,middleware.2.0" != selectedNames {
        t.Fatalf("expected the lower priority outside the static server and the default inside it, got %s", selectedNames)
    }
}

func TestHttpMiddlewareUse_RefusesANilFactory(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    testhelper.AssertPanicsWithError(t, func() {
        middleware.Use(applicationcontract.MiddlewareRegistration{Factories: []MiddlewareFactory{nil}})
    }, "middleware factory is nil in a middleware registration")
}

func TestHttpMiddlewareRegistrar_AModuleRegistersAFactoryThroughTheContract(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    var builtAgainst kernelcontract.Kernel

    var registrar applicationcontract.HttpMiddlewareRegistrar = middleware
    registrar.Use(applicationcontract.MiddlewareRegistration{Factories: []MiddlewareFactory{func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
        builtAgainst = kernelInstance

        return passThroughMiddleware
    }}})

    kernelInstance := newTestKernel()
    middleware.all(kernelInstance)

    if builtAgainst != kernelInstance {
        t.Fatalf("expected the factory built against the kernel the pipeline was built with")
    }
}

func TestHttpMiddlewareUse_AFactoryTakesThePriorityOfItsRegistration(t *testing.T) {
    middleware := newHttpMiddlewareUnderTest(t)

    middleware.Use(applicationcontract.MiddlewareRegistration{Priority: -2000, Factories: []MiddlewareFactory{passThroughFactory}})

    middleware.all(newTestKernel())

    if selectedNames := strings.Join(middleware.LastBuildReport().SelectedNames(), ","); "factory.1.-2000,static" != selectedNames {
        t.Fatalf("expected the factory outside the static server, got %s", selectedNames)
    }
}
