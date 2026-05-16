package http

import (
    "context"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())

    return runtime.New(context.Background(), scope, serviceContainer)
}
