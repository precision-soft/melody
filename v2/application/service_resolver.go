package application

import (
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

/* ServiceProcessRole resolves to the process role string (config.RoleWeb, config.RoleWorker or config.RoleAll) so services can gate background work without reaching back to the application instance. */
const ServiceProcessRole = "service.application.process_role"

func ProcessRoleMustFromContainer(serviceContainer containercontract.Container) string {
    return container.MustFromResolver[string](serviceContainer, ServiceProcessRole)
}

func ProcessRoleMustFromResolver(resolver containercontract.Resolver) string {
    return container.MustFromResolver[string](resolver, ServiceProcessRole)
}
