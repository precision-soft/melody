package amqp

const (
    ParameterDsn      = "melody.amqp.dsn"
    ParameterExchange = "melody.amqp.exchange"
    ParameterPrefetch = "melody.amqp.prefetch"
)

type ParameterRegistrar interface {
    RegisterParameter(name string, value any)
}

func RegisterDefaultParameters(registrar ParameterRegistrar) {
    registrar.RegisterParameter(ParameterDsn, "amqp://guest:guest@localhost:5672/")
    registrar.RegisterParameter(ParameterPrefetch, 10)
}
