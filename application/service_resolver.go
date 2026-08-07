package application

import (
    applicationcontract "github.com/precision-soft/melody/application/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/internal"
)

/* ServiceProcessRole resolves to the process role string (config.RoleWeb, config.RoleWorker or config.RoleAll) so services can gate background work without reaching back to the application instance. */
const ServiceProcessRole = "service.application.process_role"

func ProcessRoleMustFromContainer(serviceContainer containercontract.Container) string {
    return container.MustFromResolver[string](serviceContainer, ServiceProcessRole)
}

func ProcessRoleMustFromResolver(resolver containercontract.Resolver) string {
    return container.MustFromResolver[string](resolver, ServiceProcessRole)
}

/* ServiceProcessContext resolves to the console run's ProcessContext — its generated process id and start moment, the console counterpart of http.ServiceRequestContext. The cli entry point installs it into the run's scope, so it takes a resolver, not the container: the root container never carries it, and an http process carries the request context instead. */
const ServiceProcessContext = "service.application.process_context"

func ProcessContextMustFromResolver(resolver containercontract.Resolver) applicationcontract.ProcessContext {
    return container.MustFromResolver[applicationcontract.ProcessContext](resolver, ServiceProcessContext)
}

/* ProcessContextFromResolver is the error-tolerant form of ProcessContextMustFromResolver, for code that runs on both process shapes and treats the process context as optional: absence — an http process, the root container — answers nil. */
func ProcessContextFromResolver(resolver containercontract.Resolver) applicationcontract.ProcessContext {
    processContextInstance, err := container.FromResolver[applicationcontract.ProcessContext](resolver, ServiceProcessContext)
    if nil != err || true == internal.IsNilInterface(processContextInstance) {
        return nil
    }

    return processContextInstance
}
