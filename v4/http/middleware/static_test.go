package middleware

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/http/static"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func newStaticTestRuntime() containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger(), container.OverrideOptions{})

    return runtime.NewRequest(context.Background(), scope)
}

func TestStaticMiddleware_DoesNotDuplicateContentType(t *testing.T) {
    directory := t.TempDir()

    if writeErr := os.WriteFile(directory+"/style.css", []byte("body{color:red}"), 0o640); nil != writeErr {
        t.Fatalf("write file error: %v", writeErr)
    }

    config := static.Config{PublicDirectory: directory, CacheDisabled: true}

    handler := StaticMiddleware(config)(
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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

func TestStaticMiddleware_ReportsOnTheRuntimesLogger(t *testing.T) {
    config := static.Config{PublicDirectory: t.TempDir(), CacheDisabled: true}

    nextCalled := false
    handler := StaticMiddleware(config)(
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            nextCalled = true

            return nil, nil
        },
    )

    logger := &staticRecordingLogger{Logger: logging.NewNopLogger()}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logger, container.OverrideOptions{})

    request := testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/missing.css")

    if _, err := handler(runtime.NewRequest(context.Background(), scope), httptest.NewRecorder(), request); nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if false == nextCalled {
        t.Fatalf("expected a missing file to fall through to next")
    }

    if 0 == len(logger.messages) {
        t.Fatalf("expected the serve to report the missing file on the runtime's logger")
    }
}

type staticRecordingLogger struct {
    loggingcontract.Logger
    messages []string
}

func (instance *staticRecordingLogger) Debug(message string, context loggingcontract.Context) {
    instance.messages = append(instance.messages, message)
}

func (instance *staticRecordingLogger) Info(message string, context loggingcontract.Context) {
    instance.messages = append(instance.messages, message)
}

func (instance *staticRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.messages = append(instance.messages, message)
}
