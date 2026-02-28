package contract

type RegisterOptions struct {
    AlsoRegisterType         bool
    TypeRegistrationIsStrict bool
}

type RegisterOption func(option *RegisterOptions)

type Registrar interface {
    Register(serviceName string, provider any, options ...RegisterOption) error

    MustRegister(serviceName string, provider any, options ...RegisterOption)
}
