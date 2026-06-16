package middleware

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/http/static"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newStaticTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())

    return runtime.New(context.Background(), scope, serviceContainer)
}

func TestStaticMiddleware_DoesNotDuplicateContentType(t *testing.T) {
    dir := t.TempDir()

    if writeErr := os.WriteFile(dir+"/style.css", []byte("body{color:red}"), 0o640); nil != writeErr {
        t.Fatalf("write file error: %v", writeErr)
    }

    options := static.NewOptions(
        static.NewFileServerConfig(static.ModeFilesystem, dir, "index.html", "", false, 0, false),
        "",
        nil,
    )

    handler := StaticMiddleware(options)(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            t.Fatalf("static middleware should have served the file, not called next")

            return nil, nil
        },
    )

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/style.css")

    response, err := handler(newStaticTestRuntime(), httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if nil == response {
        t.Fatalf("expected a served response")
    }

    contentTypes := response.Headers()["Content-Type"]
    if 1 != len(contentTypes) {
        t.Fatalf("expected exactly one Content-Type header, got %d: %v", len(contentTypes), contentTypes)
    }

    if "text/css; charset=utf-8" != contentTypes[0] {
        t.Fatalf("expected the file's Content-Type to win, got: %q", contentTypes[0])
    }
}
