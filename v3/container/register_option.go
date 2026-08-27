package container

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func WithoutTypeRegistration() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.AlsoRegisterType = false
    }
}

func WithTypeRegistration(isStrict bool) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.AlsoRegisterType = true
        option.TypeRegistrationIsStrict = isStrict
    }
}

/* WithCollectionPriority orders the registration inside AllImplementing collections: a higher priority is collected — and therefore dispatched by whatever consumes the collection — earlier. The unset priority is zero, so a negative one sorts after every service that declared nothing; registrations sharing a priority keep the stable type-and-name order, so adding a priority to one service never reshuffles the rest. Only a type-registered service can be collected, so the option is meaningless together with WithoutTypeRegistration. */
func WithCollectionPriority(priority int) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.CollectionPriority = priority
    }
}

/* Replacing admits a SCOPED registration whose name — or whose registered type — the container already holds. Without it the collision is refused where it is made, because a name that means two things depending on where it is asked from is the ambiguity the two lifetimes exist to keep apart. It is read only by the scoped registration paths: a container registration declaring it gains nothing, and takes a scoped-owned name only when the scoped side itself was declared Replacing.

   Declaring it where nothing collides is not inert: the waiver is remembered, and a container registration of the same name arriving later is admitted without a declaration of its own. The waiver likewise covers the name and the registered type together — a scoped registration admitted for its name also shadows, inside every scope, the type-keyed resolution of whichever container service shares the type; a registration that means only the name opts out of the type with WithoutTypeRegistration. */
func Replacing() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.ReplacesContainerService = true
    }
}

/* WithTeardownDependency declares that this registration must be CLOSED BEFORE the named container services, without resolving them. It is the declarative form of the edge the container otherwise writes by itself: a provider that resolves another service while it is being built has that dependency recorded at the moment of the resolution, and the teardown then closes the dependent first. A provider that CAPTURES an already-built collaborator resolves nothing, so no edge exists and the teardown order it needs is decided by creation order instead — which is not an ordering at all, only a coincidence.

   Resolving remains the better door wherever it is possible, because an edge derived from the resolution cannot fall out of step with what the provider actually uses, while a declared one is a second place to keep in sync. This one exists for the registration that has nothing to resolve: an instance built before the container, published through a closure that only hands it back.

   The declaration composes — two calls add to one list — and orders teardown alone. It does not build the named service, does not make it exist, and takes no part in cycle detection during resolution; an edge naming a service that was never created is dropped by the teardown walk, so naming an optional collaborator is not an error. An empty name is refused where the registration is made, and so is a service naming itself. It is read by the container registration paths only: RegisterScoped refuses it, because a scope keeps its own teardown graph, built per scope from the resolutions that scope actually made. */
func WithTeardownDependency(serviceNames ...string) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.TeardownDependencyNames = append(option.TeardownDependencyNames, serviceNames...)
    }
}

/* WithReplacesContainerService admits a SCOPED registration whose name — or whose registered type — the container already claims, so the scoped one answers inside a scope while the container's keeps its lifetime outside one. Without it the overlap is refused where it is made, because a scoped service silently shadowing a container singleton is a wiring mistake whose symptom appears one lifetime away from its cause; the wiring generator emits this option for exactly the scoped-shadows-container registrations its scan finds, so a deliberate shadow boots instead of panicking. It admits substitution, not decoration: a scoped provider that resolves the name it replaces re-enters itself and is reported as the circular dependency it is. The container-level registration paths do not read it. */
func WithReplacesContainerService() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.ReplacesContainerService = true
    }
}

func buildRegisterServiceOption() *containercontract.RegisterOptions {
    return &containercontract.RegisterOptions{
        AlsoRegisterType:         true,
        TypeRegistrationIsStrict: true,
    }
}

func applyRegisterServiceOptions(options []containercontract.RegisterOption) *containercontract.RegisterOptions {
    merged := buildRegisterServiceOption()
    for _, optionFunc := range options {
        if nil == optionFunc {
            continue
        }
        optionFunc(merged)
    }
    return merged
}
