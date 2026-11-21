package report

import (
    "context"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

func exampleTestRuntime() melodycontainercontract.Runtime {
    return exampleTestRuntimeUnder(context.Background())
}

func exampleTestRuntimeUnder(ctx context.Context) melodycontainercontract.Runtime {
    return melodyruntime.NewRun(ctx, melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}
