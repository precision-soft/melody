//go:build !melody_static_embedded

package application

import (
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/container"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    "github.com/precision-soft/melody/v2/logging"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func newStaticExclusionTestConfiguration(t *testing.T, values map[string]string) configcontract.Configuration {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    return configuration
}

func newStaticExclusionTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())

    return runtime.New(context.Background(), scope, serviceContainer)
}

/* the pipeline hands back the middlewares outermost first, which is the order the http kernel wraps them in */
func composeStaticExclusionTestChain(chain httpMiddlewareChain, handler httpcontract.Handler) httpcontract.Handler {
    for index := len(chain) - 1; 0 <= index; index-- {
        handler = chain[index](handler)
    }

    return handler
}

func newStaticExclusionTestPublicDirectory(t *testing.T) string {
    t.Helper()

    publicDirectory := t.TempDir()

    writeErr := os.WriteFile(filepath.Join(publicDirectory, "index.html"), []byte("served from disk"), 0o640)
    if nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    makeErr := os.MkdirAll(filepath.Join(publicDirectory, "private"), 0o750)
    if nil != makeErr {
        t.Fatalf("unexpected make directory error: %v", makeErr)
    }

    writeErr = os.WriteFile(filepath.Join(publicDirectory, "private", "report.json"), []byte(`{"secret":true}`), 0o640)
    if nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    return publicDirectory
}

func TestNewStaticFileServerOptions_ExcludedPrefixReachesTheMiddlewareRegisteredWithUse(t *testing.T) {
    /* the built-in file server is the outermost middleware, so a path it answers is a path the application's own chain never sees. The exclusion is the only channel this instance has, because it is built inside the application rather than registered by it. */
    publicDirectory := newStaticExclusionTestPublicDirectory(t)

    configuration := newStaticExclusionTestConfiguration(t, map[string]string{
        config.PublicDirKey:           publicDirectory,
        config.StaticExcludedPathsKey: "/private",
        config.StaticEnableCacheKey:   "false",
    })

    httpMiddleware := NewHttpMiddleware(
        newStaticFileServerOptions(nil, configuration),
        configuration,
    )

    reached := false

    httpMiddleware.Use(func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            reached = true

            return next(runtimeInstance, writer, request)
        }
    })

    answeredByTheApplication := false

    handler := composeStaticExclusionTestChain(
        httpMiddleware.all(newTestKernel()),
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            answeredByTheApplication = true

            return nil, nil
        },
    )

    response, err := handler(
        newStaticExclusionTestRuntime(),
        httptest.NewRecorder(),
        testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/private/report.json"),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == reached {
        t.Fatalf("expected the excluded prefix to reach the middleware registered with Use")
    }

    if false == answeredByTheApplication {
        t.Fatalf("expected the excluded prefix to reach the application handler")
    }

    if nil != response {
        t.Fatalf("expected no response from the file server for an excluded prefix")
    }
}

func TestNewStaticFileServerOptions_PathOutsideTheExclusionIsStillServedFromDisk(t *testing.T) {
    publicDirectory := newStaticExclusionTestPublicDirectory(t)

    configuration := newStaticExclusionTestConfiguration(t, map[string]string{
        config.PublicDirKey:           publicDirectory,
        config.StaticExcludedPathsKey: "/private",
        config.StaticEnableCacheKey:   "false",
    })

    httpMiddleware := NewHttpMiddleware(
        newStaticFileServerOptions(nil, configuration),
        configuration,
    )

    reached := false

    httpMiddleware.Use(func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            reached = true

            return next(runtimeInstance, writer, request)
        }
    })

    handler := composeStaticExclusionTestChain(
        httpMiddleware.all(newTestKernel()),
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            t.Fatalf("expected the file server to answer a path outside the exclusion")

            return nil, nil
        },
    )

    response, err := handler(
        newStaticExclusionTestRuntime(),
        httptest.NewRecorder(),
        testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/index.html"),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if true == reached {
        t.Fatalf("expected the file server to answer before the middleware registered with Use")
    }

    if nil == response {
        t.Fatalf("expected the file server to answer with a response")
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected status 200, got %d", response.StatusCode())
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if "served from disk" != string(body) {
        t.Fatalf("expected the file body, got %q", string(body))
    }
}
