package contract

import (
    "reflect"
)

type RegisterOptions struct {
    AlsoRegisterType         bool
    TypeRegistrationIsStrict bool
    /* CollectionPriority orders this registration inside AllImplementing collections: a higher priority is collected earlier. Registrations sharing a priority keep the stable type-and-name order. */
    CollectionPriority int
    /* ReplacesContainerService admits a scoped registration whose name or type the container already claims: the scoped one answers inside a scope and the container's outside. Without it the collision is refused. It admits substitution, not decoration: a scoped provider resolving the name it replaces is reported as a circular dependency. */
    ReplacesContainerService bool
    /* TeardownDependencyNames are the container services this registration is closed before, for teardown order alone, for a provider that captures a pre-built collaborator instead of resolving it. It builds nothing and does not enter the cycle report; an edge to a service never created is dropped. */
    TeardownDependencyNames []string
    /* TeardownDependencyTypes are the same declaration keyed by type; T and *T name the same node. */
    TeardownDependencyTypes []reflect.Type
}

type RegisterOption func(option *RegisterOptions)

type Registrar interface {
    Register(serviceName string, provider any, options ...RegisterOption) error

    MustRegister(serviceName string, provider any, options ...RegisterOption)
}
