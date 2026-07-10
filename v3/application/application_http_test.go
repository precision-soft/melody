package application

import (
    "context"
    nethttp "net/http"
    "testing"
    "time"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestApplicationRegisterHttpRoute_AppendsRegistrarBeforeBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterHttpRoute(
        nethttp.MethodGet,
        "/test",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    if 1 != len(applicationInstance.httpRouteRegistrars) {
        t.Fatalf("expected 1 registrar, got %d", len(applicationInstance.httpRouteRegistrars))
    }
}

func TestApplicationRegisterHttpRoute_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.RegisterHttpRoute(
            nethttp.MethodGet,
            "/test",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, nil
            },
        )
    })
}

func TestApplicationRegisterHttpMiddlewares_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.RegisterHttpMiddlewares(func(next httpcontract.Handler) httpcontract.Handler {
            return next
        })
    })
}

func TestApplicationRegisterHttpMiddlewareFactories_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.RegisterHttpMiddlewareFactories(
            func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
                return func(next httpcontract.Handler) httpcontract.Handler {
                    return next
                }
            },
        )
    })
}

/** @info http.Server.Shutdown neither cancels an in-flight request's context nor tracks a hijacked connection, so a Server-Sent Events stream or a websocket blocks the entire shutdown timeout and is then cut mid-flight. The hook is what lets the application release those handlers, and net/http runs it the moment Shutdown begins. */
func TestOnHttpShutdown_HooksRunWhenTheServerShutsDown(t *testing.T) {
    released := make(chan struct{})

    httpServer := &nethttp.Server{Handler: nethttp.NotFoundHandler()}
    httpServer.RegisterOnShutdown(func() { close(released) })

    shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    if shutdownErr := httpServer.Shutdown(shutdownContext); nil != shutdownErr {
        t.Fatalf("shutdown: %v", shutdownErr)
    }

    select {
    case <-released:
    case <-time.After(2 * time.Second):
        t.Fatalf("the shutdown hook never ran")
    }
}

/** @info The hook list reaches the server: a nil hook is rejected outright, and a registered one is kept. */
func TestOnHttpShutdown_RejectsNilAndKeepsTheHook(t *testing.T) {
    applicationInstance := &Application{}

    applicationInstance.OnHttpShutdown(func() {})
    if 1 != len(applicationInstance.httpShutdownHooks) {
        t.Fatalf("expected the hook to be registered, got %d", len(applicationInstance.httpShutdownHooks))
    }

    defer func() {
        if nil == recover() {
            t.Fatalf("a nil shutdown hook must panic")
        }
    }()

    applicationInstance.OnHttpShutdown(nil)
}
