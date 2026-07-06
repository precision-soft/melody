package http

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestRouter_HandleAndServeHttp_HappyPath(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    req := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if 200 != rec.Code {
        t.Fatalf("unexpected status")
    }
    if "ok" != rec.Body.String() {
        t.Fatalf("unexpected body")
    }
}

func TestRouter_MethodNotAllowed(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    req := httptest.NewRequest(nethttp.MethodPost, "/hello", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if 405 != rec.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_AllowHeaderRespectsMethodPolicy(t *testing.T) {
    newGetOnlyKernel := func() *Kernel {
        router := NewRouter()
        router.Handle(
            nethttp.MethodGet,
            "/hello",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return TextResponse(200, "ok"), nil
            },
        )

        return NewKernel(router)
    }

    allowFor := func(kernel *Kernel) string {
        handler := kernel.ServeHttp(newHttpTestContainer())
        req := httptest.NewRequest(nethttp.MethodPost, "/hello", nil)
        rec := httptest.NewRecorder()
        handler.ServeHTTP(rec, req)

        if 405 != rec.Code {
            t.Fatalf("expected 405, got %d", rec.Code)
        }

        return rec.Header().Get("Allow")
    }

    /* default policy advertises the synthetic OPTIONS and HEAD it actually honors */
    defaultAllow := allowFor(newGetOnlyKernel())
    if false == strings.Contains(defaultAllow, nethttp.MethodOptions) || false == strings.Contains(defaultAllow, nethttp.MethodHead) {
        t.Fatalf("default policy Allow must advertise OPTIONS and HEAD, got %q", defaultAllow)
    }

    /* with both policy flags off, OPTIONS and HEAD in fact return 405, so Allow must not promise them */
    restricted := newGetOnlyKernel()
    restricted.options.MethodPolicy.AutomaticOptions = false
    restricted.options.MethodPolicy.HeadFallbackToGet = false
    restrictedAllow := allowFor(restricted)

    if true == strings.Contains(restrictedAllow, nethttp.MethodOptions) {
        t.Fatalf("Allow must not advertise OPTIONS when AutomaticOptions is off, got %q", restrictedAllow)
    }
    if true == strings.Contains(restrictedAllow, nethttp.MethodHead) {
        t.Fatalf("Allow must not advertise HEAD when HeadFallbackToGet is off, got %q", restrictedAllow)
    }
    if false == strings.Contains(restrictedAllow, nethttp.MethodGet) {
        t.Fatalf("Allow must still advertise the real GET method, got %q", restrictedAllow)
    }
}

func TestRouter_NotFound(t *testing.T) {
    router := NewRouter()

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    req := httptest.NewRequest(nethttp.MethodGet, "/missing", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if 404 != rec.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_PanicConvertedTo500(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/panic",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            exception.Panic(exception.NewError("boom", nil, nil))
            return nil, nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    req := httptest.NewRequest(nethttp.MethodGet, "/panic", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if 500 != rec.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_HandlerErrorConvertedTo500(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/err",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, errors.New("handler error")
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    req := httptest.NewRequest(nethttp.MethodGet, "/err", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if 500 != rec.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_ParamExtraction(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/user/:id",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            value, exists := request.Param("id")
            if false == exists {
                return TextResponse(500, "missing id"), nil
            }

            return TextResponse(200, value), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    req := httptest.NewRequest(nethttp.MethodGet, "/user/123", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if 200 != rec.Code {
        t.Fatalf("unexpected status")
    }
    if "123" != rec.Body.String() {
        t.Fatalf("unexpected body")
    }
}
