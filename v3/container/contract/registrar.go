package contract

type RegisterOptions struct {
    AlsoRegisterType         bool
    TypeRegistrationIsStrict bool
    /* CollectionPriority orders this registration inside AllImplementing collections: a higher priority is collected earlier. Registrations sharing a priority keep the stable type-and-name order. */
    CollectionPriority int
    /* ReplacesContainerService admits a SCOPED registration whose name — or whose registered type — the container already claims; the container-level registration paths do not read it. Without it the collision is refused at the point it is made: a scoped service silently shadowing a container singleton is a wiring mistake whose symptom appears one lifetime away from its cause. With it, the scoped registration answers inside a scope and the container's keeps its own lifetime outside one. Declared where nothing collides yet, the waiver stands: a container registration of the same name arriving later is admitted without declaring anything itself.

       It admits substitution, not decoration. A scoped provider that resolves the name it is replacing re-enters itself, and the resolution is reported as the circular dependency it is; a decorator has to take the decorated service under its own name. */
    ReplacesContainerService bool
}

type RegisterOption func(option *RegisterOptions)

type Registrar interface {
    Register(serviceName string, provider any, options ...RegisterOption) error

    MustRegister(serviceName string, provider any, options ...RegisterOption)
}
