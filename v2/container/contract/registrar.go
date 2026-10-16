package contract

type RegisterOptions struct {
    AlsoRegisterType         bool
    TypeRegistrationIsStrict bool
    /* ReplacesContainerService admits a scoped registration whose name or registered type the container already claims; the container registration paths do not read it, and without it the collision is refused where it is made. The waiver is remembered, so a container registration of the same name arriving later is admitted. It admits substitution, not decoration: a scoped provider that resolves the name it replaces is reported as a circular dependency. */
    ReplacesContainerService bool
}

type RegisterOption func(option *RegisterOptions)

type Registrar interface {
    Register(serviceName string, provider any, options ...RegisterOption) error

    MustRegister(serviceName string, provider any, options ...RegisterOption)
}
