package application

import (
    "context"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* ServiceProcessRole resolves to the process role string (config.RoleWeb, config.RoleWorker or config.RoleAll) so services can gate background work without reaching back to the application instance. */
const ServiceProcessRole = "service.application.process_role"

func ProcessRoleMustFromResolver(ctx context.Context, resolver containercontract.Resolver) string {
    return container.MustFromResolver[string](ctx, resolver, ServiceProcessRole)
}

/* ServiceProcessContext resolves to the console run's ProcessContext, the counterpart of http.ServiceRequestContext. The cli entry point installs it into the run's scope, so it takes a resolver, not the container. */
const ServiceProcessContext = "service.application.process_context"

/* ProcessContextFromRuntime answers the console run's process context; a runtime that is not a run's answers an error under containercontract.ErrRuntimeKindRefused naming both kinds. */
func ProcessContextFromRuntime(runtimeInstance containercontract.Runtime) (applicationcontract.ProcessContext, error) {
    if kindErr := containercontract.RefuseRuntimeKind(runtimeInstance, "ProcessContextFromRuntime", []containercontract.Kind{containercontract.KindRun}); nil != kindErr {
        return nil, kindErr
    }

    return container.FromResolver[applicationcontract.ProcessContext](runtimeInstance.Context(), runtimeInstance.Scope(), ServiceProcessContext)
}

func ProcessContextMustFromRuntime(runtimeInstance containercontract.Runtime) applicationcontract.ProcessContext {
    processContext, err := ProcessContextFromRuntime(runtimeInstance)
    if nil != err {
        exception.Panic(exception.NewError("could not resolve the process context", nil, err))
    }

    return processContext
}
