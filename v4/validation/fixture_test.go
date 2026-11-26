package validation

import (
    goerrors "errors"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "context"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

func pointerOf(value string) *string {
    return &value
}

func recoveredPanicContext(callback func()) (context exceptioncontract.Context) {
    defer func() {
        if exceptionErr, isExceptionError := recover().(*exception.Error); true == isExceptionError && nil != exceptionErr {
            context = exceptionErr.Context()
        }
    }()

    callback()

    return nil
}

func testValidationRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}

func verifiedTypeFaults(t *testing.T, err error) []map[string]any {
    t.Helper()

    if nil == err {
        t.Fatalf("expected the type to be refused")
    }

    if false == goerrors.Is(err, ErrTypeDeclarationRefused) {
        t.Fatalf("expected the refusal to wrap ErrTypeDeclarationRefused, got %v", err)
    }

    var exceptionErr *exception.Error
    if false == goerrors.As(err, &exceptionErr) {
        t.Fatalf("expected an *exception.Error, got %T", err)
    }

    faults, isList := exceptionErr.Context()["faults"].([]map[string]any)
    if false == isList {
        t.Fatalf("expected the faults list in the context, got %#v", exceptionErr.Context())
    }

    return faults
}
