package contract

import (
    "context"
    "reflect"
)

/* RegisterOptions is what a registration declares beyond its name and provider. The zero value is the default: the service is registered under its name and, strictly, under the type its provider declares, collected at priority zero, substituting nothing, walked by the armed teardown, and closed through the doors its value carries. */
type RegisterOptions struct {
    /* TypeRegistrationDisabled keeps the service off the by-type index, so it is resolved by name alone. */
    TypeRegistrationDisabled bool
    /* TypeRegistrationRelaxed admits a second registration under the same type instead of refusing it; by-type resolution then refuses every one of them as ambiguous, while a collection gathers them all. It is read only while the type is registered. */
    TypeRegistrationRelaxed bool
    /* CollectionPriority orders this registration inside AllImplementing collections: a higher priority is collected earlier. Registrations sharing a priority keep the stable type-and-name order. Only a type-registered service can be collected. */
    CollectionPriority int
    /* ReplacesContainerService admits a scoped registration whose name or type the container already claims: the scoped one answers inside a scope and the container's outside. Without it the collision is refused. Only the scoped registration paths read it. The waiver is remembered, so a later container registration of the same name is admitted, and it covers the registered type too unless TypeRegistrationDisabled is set. It admits substitution, not decoration: a scoped provider resolving the name it replaces is reported as a circular dependency. */
    ReplacesContainerService bool
    /* TeardownDependencyNames are the container services this registration is closed before, for teardown order alone, for a provider that captures a pre-built collaborator instead of resolving it; resolving is preferred wherever possible. It builds nothing and does not enter the cycle report; an edge to a service never created is dropped. An empty name and the service's own name are refused, and a scoped registration refuses the list. Under ArmParallelTeardown a name nothing registered is refused with ErrTeardownDependencyWasNeverRegistered and a scoped one with ErrTeardownDependencyIsScoped. */
    TeardownDependencyNames []string
    /* TeardownDependencyTypes are the same declaration keyed by type, written reflect.TypeFor[T](); T and *T name the same node. A nil entry is refused. A type nothing, or more than one service, is registered under orders nothing on the default path, and is refused under ArmParallelTeardown with ErrTeardownDependencyWasNeverRegistered or ErrTeardownDependencyTypeIsAmbiguous; a scoped-only type is refused with ErrTeardownDependencyIsScoped, and a registration filing the type it declares against with ErrTeardownDependencyIsSelf. */
    TeardownDependencyTypes []reflect.Type
    /* TeardownReflectionDisabled keeps the armed teardown's walk out of this service's memory, for a service whose own goroutines rewrite its pointer fields: its value is not walked where it is filed nor at ArmParallelTeardown, and a walk that reaches it records the edge and does not enter it, so what it holds is declared with TeardownDependencyNames instead. It also covers a value installed over the name; the scoped registration paths do not read it. */
    TeardownReflectionDisabled bool
    /* Closer closes the service in place of the Close and CloseWithContext doors its value carries; the zero Closer is none. A scoped registration refuses one. */
    Closer Closer
}

/* Closer is the door a registration closes its value through, handed the teardown's remaining budget: the door for a value of a type the application does not own, whose own close cannot be bounded. ValueType is the type Close takes, which the provider's declared type must be assignable to. It is built with container.NewCloser; a Closer with only one of its two fields set is refused at registration. */
type Closer struct {
    ValueType reflect.Type
    Close     func(closeContext context.Context, value any) error
}

type Registrar interface {
    Register(serviceName string, provider any, options RegisterOptions) error

    MustRegister(serviceName string, provider any, options RegisterOptions)
}
