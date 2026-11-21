package validation

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "context"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

/* pointerOf builds the optional value every constraint has to skip rather than refuse; every constraint suite in the package reaches it from here. */
func pointerOf(value string) *string {
    return &value
}

/* recoveredPanicContext answers the context of the *exception.Error the callback panics with, nil when it does not. */
func recoveredPanicContext(callback func()) (context exceptioncontract.Context) {
    defer func() {
        if exceptionErr, isExceptionError := recover().(*exception.Error); true == isExceptionError && nil != exceptionErr {
            context = exceptionErr.Context()
        }
    }()

    callback()

    return nil
}

/* testValidationRuntime is the runtime a validation under test runs under */
func testValidationRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}
