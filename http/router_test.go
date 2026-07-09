package http

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/clock"
    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

type testEnvironmentSource struct {
    values map[string]string
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func newHttpTestContainer() containercontract.Container {
    return newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
}

func newHttpTestContainerWithSessionStorage(storage sessioncontract.Storage) containercontract.Container {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, err := config.NewEnvironment(
                &testEnvironmentSource{
                    values: map[string]string{
                        config.EnvKey: config.EnvDevelopment,
                    },
                },
            )
            if nil != err {
                return nil, err
            }

            return config.NewConfiguration(environment, "/tmp/melody")
        },
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(storage, 30*time.Minute), nil
        },
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(clock.NewSystemClock()), nil
        },
    )

    return serviceContainer
}

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

/** @info A requirement is a whitelist for one path segment. Anchors bind looser than alternation, so concatenating "^" and "$" onto "en|de|fr" yields (^en)|(de)|(fr$) — which accepts "aden", "frfr" and any string ending in "fr". The requirement must be wrapped in a non-capturing group so the anchors apply to the whole alternation. */
func TestRouter_RequirementWithAlternationMatchesTheWholeSegment(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions(
        "/shop/:locale/list",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
        NewRouteOptions(
            "shop.list",
            []string{nethttp.MethodGet},
            "",
            nil,
            map[string]string{"locale": "en|de|fr"},
            nil,
            nil,
            0,
            nil,
        ),
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    accepted := []string{"en", "de", "fr"}
    for _, locale := range accepted {
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/shop/"+locale+"/list", nil))

        if 200 != recorder.Code {
            t.Fatalf("expected the whitelisted locale %q to route, got %d", locale, recorder.Code)
        }
    }

    rejected := []string{"aden", "frfr", "xxde", "enen", "en..%2fetc%2fpasswd"}
    for _, locale := range rejected {
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/shop/"+locale+"/list", nil))

        if 200 == recorder.Code {
            t.Fatalf("expected the non-whitelisted locale %q to be refused, but it routed", locale)
        }
    }
}
