package main

import (
    "context"
    "io/fs"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "reflect"
    "strings"
    "testing"
    "testing/fstest"
    "time"

    melodyamqp "github.com/precision-soft/melody/integrations/amqp/v4"
    melodycron "github.com/precision-soft/melody/integrations/cron/v4"
    "github.com/precision-soft/melody/v4/.example/config"

    "github.com/precision-soft/melody/v4/application"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/kernel"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/openapi"
)

func invoke[T any](door func(T)) {
    var zero T
    door(zero)
}

func invokeInstall[T any](door func(T, func(kind string, name string)), recorder func(kind string, name string)) {
    var zero T
    door(zero, recorder)
}

func invokeNewKernel[T any](
    door func(T, configcontract.Configuration, containercontract.Container, httpcontract.Router, eventcontract.EventDispatcher, clockcontract.Clock) kernelcontract.Kernel,
) {
    var zero T
    door(zero, nil, nil, nil, nil, nil)
}

func requireFrameworkOnlyRefusal(t *testing.T, expectedMessage string, call func()) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the refusal %q, the door was entered", expectedMessage)
        }

        refusal, isRefusal := recovered.(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected a melody exception, got %T: %v", recovered, recovered)
        }

        if expectedMessage != refusal.Message() {
            t.Fatalf("expected the refusal %q, got %q", expectedMessage, refusal.Message())
        }
    }()

    call()
}

func requireTokenRefusesEmptyStructLiteral(t *testing.T, doorName string, door any) {
    t.Helper()

    doorType := reflect.TypeOf(door)
    if 0 == doorType.NumIn() {
        t.Fatalf("expected %s to take the seam token", doorName)
    }

    if true == reflect.TypeOf(struct{}{}).AssignableTo(doorType.In(0)) {
        t.Fatalf("expected the token of %s not to admit struct{}{}", doorName)
    }
}

func TestFrameworkDoors_RefuseATokenTheFrameworkDidNotBuild(t *testing.T) {
    applicationInstance := application.NewApplication(
        context.Background(),
        application.ApplicationConfig{
            EnvironmentFiles: embeddedEnvironmentFiles,
            PublicFiles:      embeddedPublicFiles,
        },
    )

    configuration, isConfiguration := applicationInstance.Configuration().(*melodyconfig.Configuration)
    if false == isConfiguration {
        t.Fatalf("expected the application configuration to be *config.Configuration, got %T", applicationInstance.Configuration())
    }

    registry := openapi.NewRegistry()
    routeRegistry := melodyhttp.NewRouteRegistry()

    requireTokenRefusesEmptyStructLiteral(t, "Configuration.MarkServing", configuration.MarkServing)
    requireTokenRefusesEmptyStructLiteral(t, "Registry.MarkServing", registry.MarkServing)
    requireTokenRefusesEmptyStructLiteral(t, "RouteRegistry.InstallBootCollisionRecorder", routeRegistry.InstallBootCollisionRecorder)
    requireTokenRefusesEmptyStructLiteral(t, "kernel.NewKernel", kernel.NewKernel)

    requireFrameworkOnlyRefusal(t, "configuration MarkServing is called by the framework only", func() {
        invoke(configuration.MarkServing)
    })

    requireFrameworkOnlyRefusal(t, "openapi registry MarkServing is called by the framework only", func() {
        invoke(registry.MarkServing)
    })

    requireFrameworkOnlyRefusal(t, "route registry InstallBootCollisionRecorder is called by the framework only", func() {
        invokeInstall(routeRegistry.InstallBootCollisionRecorder, func(kind string, name string) {})
    })

    requireFrameworkOnlyRefusal(t, "NewKernel is called by the framework only", func() {
        invokeNewKernel(kernel.NewKernel)
    })

    /* the .env carries the cron keys, which only a registered cron module declares */
    applicationInstance.RegisterModule(melodycron.NewModule(melodycron.ModuleConfig{}))

    kernelInstance := applicationInstance.Boot()
    installer := reflect.ValueOf(kernelInstance).MethodByName("InstallBootRuntime")
    if false == installer.IsValid() {
        t.Fatal("expected the kernel to carry InstallBootRuntime")
    }

    if true == reflect.TypeOf(struct{}{}).AssignableTo(installer.Type().In(0)) {
        t.Fatal("expected the token of InstallBootRuntime not to admit struct{}{}")
    }

    requireFrameworkOnlyRefusal(t, "kernel InstallBootRuntime is called by the framework only", func() {
        installer.Call([]reflect.Value{reflect.Zero(installer.Type().In(0)), reflect.Zero(installer.Type().In(1))})
    })

    if nil == kernelInstance.BootRuntime() {
        t.Fatal("expected the boot runtime the application installed to stand after the refused install")
    }
}

/* a loopback port nothing listens on: the boot's dial of the broker is refused at once, never held for the dial timeout */
const closedLoopbackAmqpDsn = "amqp://guest:guest@127.0.0.1:1/"

func environmentFilesWithAmqpDsn(t *testing.T, dsn string) fs.FS {
    t.Helper()

    source := embeddedEnvironmentFiles
    if nil == source {
        source = os.DirFS(".")
    }

    content, readErr := fs.ReadFile(source, ".env")
    if nil != readErr {
        t.Fatalf("read the committed .env: %v", readErr)
    }

    lines := strings.Split(string(content), "\n")
    replaced := 0
    for index, line := range lines {
        if true == strings.HasPrefix(line, "AMQP_DSN=") {
            lines[index] = "AMQP_DSN=" + dsn
            replaced++
        }
    }

    if 1 != replaced {
        t.Fatalf("expected the committed .env to set AMQP_DSN once, found %d", replaced)
    }

    return fstest.MapFS{".env": &fstest.MapFile{Data: []byte(strings.Join(lines, "\n"))}}
}

func TestApplicationHttpHandler_ServesTheHealthRouteUnderAnotherServer(t *testing.T) {
    /* the test binary's own flags would read as a cli command; without them the process runs the .env's default mode, http */
    originalArguments := os.Args
    os.Args = []string{originalArguments[0]}
    defer func() { os.Args = originalArguments }()

    applicationInstance := application.NewApplication(
        context.Background(),
        application.ApplicationConfig{
            Version:            applicationVersion,
            EnvironmentFiles:   environmentFilesWithAmqpDsn(t, closedLoopbackAmqpDsn),
            PublicFiles:        embeddedPublicFiles,
            EnvironmentKeys:    config.EnvironmentKeys(),
            ParameterOverrides: config.ParameterOverrides(),
            SecurityHeaders:    config.SecurityHeaders(),
            HttpKernel:         config.HttpKernelConfig(),
        },
    )

    config.Configure(context.Background(), applicationInstance, applicationVersion)

    handler, handlerErr := applicationInstance.HttpHandler()
    if nil != handlerErr {
        t.Fatalf("unexpected http handler error: %v", handlerErr)
    }

    if websocketRoute, found := applicationInstance.Boot().HttpRouter().RouteDefinition("example.websocket"); false == found || "/ws" != websocketRoute.Pattern() {
        t.Fatalf("expected the websocket module to register /ws, got %v %v", websocketRoute, found)
    }

    amqpDsn := applicationInstance.Configuration().MustGet("app.amqp.dsn").String()
    if ("" != amqpDsn) != applicationInstance.Boot().ServiceContainer().Has(melodyamqp.ServiceConnection) {
        t.Fatalf("expected the amqp module registered exactly beside AMQP_DSN, the dsn %q", amqpDsn)
    }

    server := httptest.NewServer(handler)

    response, requestErr := nethttp.Get(server.URL + "/health")
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }
    _ = response.Body.Close()

    if nethttp.StatusOK != response.StatusCode {
        t.Fatalf("expected the health route served, got %d", response.StatusCode)
    }

    shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancelShutdown()

    if beginErr := applicationInstance.BeginHttpShutdown(shutdownContext); nil != beginErr {
        t.Fatalf("unexpected begin error: %v", beginErr)
    }

    server.Close()

    closeContext, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancelClose()

    if closeErr := applicationInstance.CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
}
