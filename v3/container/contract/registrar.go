package contract

import (
    "reflect"
)

type RegisterOptions struct {
    AlsoRegisterType         bool
    TypeRegistrationIsStrict bool
    /* CollectionPriority orders this registration inside AllImplementing collections: a higher priority is collected earlier. Registrations sharing a priority keep the stable type-and-name order. */
    CollectionPriority int
    /* ReplacesContainerService admits a SCOPED registration whose name — or whose registered type — the container already claims; the container-level registration paths do not read it. Without it the collision is refused at the point it is made: a scoped service silently shadowing a container singleton is a wiring mistake whose symptom appears one lifetime away from its cause. With it, the scoped registration answers inside a scope and the container's keeps its own lifetime outside one. Declared where nothing collides yet, the waiver stands: a container registration of the same name arriving later is admitted without declaring anything itself.

       It admits substitution, not decoration. A scoped provider that resolves the name it is replacing re-enters itself, and the resolution is reported as the circular dependency it is; a decorator has to take the decorated service under its own name. */
    ReplacesContainerService bool
    /* TeardownDependencyNames are the container services this registration declares itself dependent on for TEARDOWN ORDER alone, so it is closed before them. It exists for the registration that cannot write its edge the ordinary way: the graph the teardown walks is recorded automatically when a provider RESOLVES another service while it is being built, and a provider that CAPTURES an already-built collaborator instead resolves nothing and therefore declares nothing. Resolving is still the better door, because it cannot fall out of step with what the provider actually uses; this one is for the pre-built instance that has nothing to resolve.

       It orders teardown and nothing else: it does not build the named service, does not make it exist, and does not enter the cycle report of a resolution. An edge naming a service that was never created is dropped by the teardown walk, so declaring a dependency on an optional service is not an error at either end. */
    TeardownDependencyNames []string
    /* TeardownDependencyTypes are the same declaration keyed by TYPE rather than by name, for the collaborator a registration knows as a type and not as a spelling — which is every service reached through GetByType, and every one whose name belongs to a module the declaring code does not own. The container files T and *T under one type, so either form names the same node.

       It carries the same meaning and the same limits as the names above, and both are written into the one graph the teardown walks. */
    TeardownDependencyTypes []reflect.Type
}

type RegisterOption func(option *RegisterOptions)

type Registrar interface {
    Register(serviceName string, provider any, options ...RegisterOption) error

    MustRegister(serviceName string, provider any, options ...RegisterOption)
}
