package contract

type ParameterModule interface {
    Module
    RegisterParameters(registrar ParameterRegistrar)
}

type ParameterRegistrar interface {
    RegisterParameter(name string, value any)

    /* RegisterSecretParameter declares a parameter holding a credential, so the commands that render the configuration redact it and every parameter whose template reads it. */
    RegisterSecretParameter(name string, value any)

    /* MarkParameterSecret marks a parameter by its name; a .env key is no parameter and is marked by declaring it Secret. */
    MarkParameterSecret(name string)
}
