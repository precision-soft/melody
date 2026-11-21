package validation

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

/* testValidationRuntime is the runtime a validation under test runs under */
func testValidationRuntime() containercontract.Runtime {
    return runtime.NewRun(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}
