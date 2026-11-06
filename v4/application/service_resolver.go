package application

import (
    "context"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* ServiceProcessRole resolves to the process role string (config.RoleWeb, config.RoleWorker or config.RoleAll) so services can gate background work without reaching back to the application instance. */
const ServiceProcessRole = "service.application.process_role"

func ProcessRoleMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) string {
    return container.MustFromResolver[string](ctx, serviceContainer, ServiceProcessRole)
}

func ProcessRoleMustFromResolver(ctx context.Context, resolver containercontract.Resolver) string {
    return container.MustFromResolver[string](ctx, resolver, ServiceProcessRole)
}

/* ServiceProcessContext resolves to the console run's ProcessContext, the counterpart of http.ServiceRequestContext. The cli entry point installs it into the run's scope, so it takes a resolver, not the container. */
const ServiceProcessContext = "service.application.process_context"

func ProcessContextMustFromResolver(ctx context.Context, resolver containercontract.Resolver) applicationcontract.ProcessContext {
    return container.MustFromResolver[applicationcontract.ProcessContext](ctx, resolver, ServiceProcessContext)
}

/* ProcessContextFromResolver is the error-tolerant form of ProcessContextMustFromResolver, for code that runs on both process shapes and treats the process context as optional: absence — an http process, the root container — answers nil. */
func ProcessContextFromResolver(ctx context.Context, resolver containercontract.Resolver) applicationcontract.ProcessContext {
    processContextInstance, err := container.FromResolver[applicationcontract.ProcessContext](ctx, resolver, ServiceProcessContext)
    if nil != err || true == internal.IsNilInterface(processContextInstance) {
        return nil
    }

    return processContextInstance
}
