package contract

type RegisterOptions struct {
    AlsoRegisterType         bool
    TypeRegistrationIsStrict bool
    /* ReplacesContainerService admits a registration whose name — or whose registered type — the other level already claims. Without it the collision is refused at the point it is made: a scoped service silently shadowing a container singleton, or a container singleton silently shadowing a scoped service, is a wiring mistake whose symptom appears one lifetime away from its cause, and which of the two is reported would otherwise depend on the order the modules happened to register in. With it, the scoped registration answers inside a scope and the container's keeps its own lifetime outside one.

    It admits substitution, not decoration. A scoped provider that resolves the name it is replacing re-enters itself, and the resolution is reported as the circular dependency it is; a decorator has to take the decorated service under its own name. */
    ReplacesContainerService bool
}

type RegisterOption func(option *RegisterOptions)

type Registrar interface {
    Register(serviceName string, provider any, options ...RegisterOption) error

    MustRegister(serviceName string, provider any, options ...RegisterOption)
}
