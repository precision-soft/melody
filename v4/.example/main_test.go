package main

import (
    "context"
    "reflect"
    "testing"

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

/* invoke is the form an application reaches a framework door through without spelling its token: the token type is inferred from the method value and its zero value passed */
func invoke[T any](door func(T)) {
    var zero T
    door(zero)
}

func invokeInstall[T any](door func(T, func(kind string, name string)), recorder func(kind string, name string)) {
    var zero T
    door(zero, recorder)
}

func invokeNewKernel[T any](
    door func(T, configcontract.Configuration, containercontract.Container, httpcontract.Router, eventcontract.EventDispatcher, clockcontract.Clock, melodyhttp.KernelConfig) kernelcontract.Kernel,
) {
    var zero T
    door(zero, nil, nil, nil, nil, nil, melodyhttp.KernelConfig{})
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

/* the first parameter of each door is a token an untyped struct{}{} cannot stand for */
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

    /* the boot runtime door sits on an unexported kernel: reflection is the only reach, and the zero token it builds is refused */
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
