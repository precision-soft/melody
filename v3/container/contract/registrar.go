package contract

import (
    "reflect"
)

type RegisterOptions struct {
    AlsoRegisterType         bool
    TypeRegistrationIsStrict bool
    /* CollectionPriority orders this registration inside AllImplementing collections: a higher priority is collected earlier. Registrations sharing a priority keep the stable type-and-name order. */
    CollectionPriority int
    /* ReplacesContainerService permits a scoped name or type to shadow a container registration, regardless of registration order. The container registration retains its lifetime outside scopes. This substitutes rather than decorates: resolving the replaced name inside its scoped provider is circular; decorators need a distinct name for the underlying service. */
    ReplacesContainerService bool
    /* TeardownDependencyNames orders this service before the named services at teardown. Prefer provider resolution to record dependencies automatically; explicit edges cover captured, prebuilt collaborators. They neither build nor require the target services, and unbuilt targets are omitted. */
    TeardownDependencyNames []string
    /* TeardownDependencyTypes declares the same teardown-only edges by canonical type. T and *T identify the same node. */
    TeardownDependencyTypes []reflect.Type
}

type RegisterOption func(option *RegisterOptions)

type Registrar interface {
    Register(serviceName string, provider any, options ...RegisterOption) error

    MustRegister(serviceName string, provider any, options ...RegisterOption)
}
