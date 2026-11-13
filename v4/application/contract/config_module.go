package contract

type ConfigModule interface {
    Module
    RegisterConfigurations(registrar ConfigRegistrar)
}

type ConfigRegistrar interface {
    RegisterConfiguration(name string, configuration any)
}
