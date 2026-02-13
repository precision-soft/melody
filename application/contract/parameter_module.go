package contract

type ParameterModule interface {
	Module
	RegisterParameters(registrar ParameterRegistrar)
}

type ParameterRegistrar interface {
	RegisterParameter(name string, value any)
}
