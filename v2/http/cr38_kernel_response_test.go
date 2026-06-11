package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func TestKernel_ResponseListenerReplacesResponseOnSuccessPath(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "original"), nil
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "replaced"))
            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    req := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusAccepted != rec.Code {
        t.Fatalf("expected listener-replaced status %d, got %d", nethttp.StatusAccepted, rec.Code)
    }

    if "replaced" != rec.Body.String() {
        t.Fatalf("expected listener-replaced body, got %q", rec.Body.String())
    }
}

func TestKernel_ResponseListenerReplacesResponseOnPanicRecoveryPath(t *testing.T) {
    router := NewRouter()
    router.Handle(
        nethttp.MethodGet,
        "/boom",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic("boom")
        },
    )

    serviceContainer := newHttpTestContainer()

    dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)
    dispatcher.AddListener(
        kernelcontract.EventKernelResponse,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            responseEvent, ok := eventValue.Payload().(*KernelResponseEvent)
            if false == ok {
                return nil
            }

            responseEvent.SetResponse(TextResponse(nethttp.StatusAccepted, "recovered-replaced"))
            return nil
        },
        0,
    )

    handler := NewKernel(router).ServeHttp(serviceContainer)

    req := httptest.NewRequest(nethttp.MethodGet, "/boom", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if nethttp.StatusAccepted != rec.Code {
        t.Fatalf("expected listener-replaced status %d on panic-recovery path, got %d", nethttp.StatusAccepted, rec.Code)
    }

    if "recovered-replaced" != rec.Body.String() {
        t.Fatalf("expected listener-replaced body on panic-recovery path, got %q", rec.Body.String())
    }
}
