package application

import (
    "context"

    container "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    runtime "github.com/precision-soft/melody/v4/container/runtime"
)

func sessionRuntime() containercontract.Runtime {
    return sessionRuntimeUnder(context.Background())
}

func sessionRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}
