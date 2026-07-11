package application

import (
    "context"
    nethttp "net/http"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
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

/** @info OnHttpShutdown copies its hooks into the net/http server once, on the main goroutine, before ListenAndServe; a hook registered after boot never reaches the server (silently dropped) and the append races, so — like every sibling registrar — it must panic once the application has booted. */
func TestOnHttpShutdown_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanics(t, func() {
        applicationInstance.OnHttpShutdown(func() {})
    })
}

/** @info net/http runs each shutdown hook on a bare `go f()`, where a panic cannot be recovered by Run's logOnRecoverAndExit and would hard-crash the process mid-drain, skipping Application.Close(). The wrapper recovers the panic through the framework logger and still marks the hook done, so a panicking hook is contained like every other extension point. */
func TestWrapHttpShutdownHook_ContainsAPanicAndStillCompletes(t *testing.T) {
    var hooksDone sync.WaitGroup

    wrapped := wrapHttpShutdownHook(
        func() {
            exception.Panic(exception.NewError("shutdown hook boom", nil, nil))
        },
        &hooksDone,
        logging.NewNopLogger(),
    )

    /* net/http runs the hook on its own goroutine; an unrecovered panic there would crash the whole test binary */
    hookReturned := make(chan struct{})
    go func() {
        wrapped()

        close(hookReturned)
    }()

    select {
    case <-hookReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("the wrapped shutdown hook never returned")
    }

    hooksDone.Wait()
}

/** @info Shutdown starts the hooks on detached goroutines and returns immediately when the only open connections are hijacked, so runHttp must join the hooks before returning. waitForHttpShutdownHooks must not report done until the registered hook has actually finished. */
func TestWaitForHttpShutdownHooks_BlocksUntilTheHookCompletes(t *testing.T) {
    var hooksDone sync.WaitGroup

    hookFinished := make(chan struct{})

    wrapped := wrapHttpShutdownHook(
        func() {
            time.Sleep(50 * time.Millisecond)

            close(hookFinished)
        },
        &hooksDone,
        logging.NewNopLogger(),
    )

    /* net/http starts each shutdown hook on its own goroutine */
    go wrapped()

    waitReturned := make(chan struct{})
    go func() {
        waitForHttpShutdownHooks(&hooksDone, context.Background())

        close(waitReturned)
    }()

    select {
    case <-waitReturned:
        select {
        case <-hookFinished:
        default:
            t.Fatalf("the wait returned before the shutdown hook completed")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("the wait never returned")
    }
}

/** @info The join is bounded by the shutdown budget: a hook that never finishes must not pin the process forever, so a spent context releases the wait. */
func TestWaitForHttpShutdownHooks_ReturnsWhenTheBudgetIsSpent(t *testing.T) {
    var hooksDone sync.WaitGroup

    /* a hook that never completes */
    hooksDone.Add(1)

    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()

    waitReturned := make(chan struct{})
    go func() {
        waitForHttpShutdownHooks(&hooksDone, ctx)

        close(waitReturned)
    }()

    select {
    case <-waitReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("the wait ignored the spent shutdown budget and hung")
    }
}
