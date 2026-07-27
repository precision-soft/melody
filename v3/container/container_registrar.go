package container

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

func (instance *container) Register(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) error {
    if "" == serviceName {
        return exception.NewError(
            "service name is required to register a service",
            nil,
            nil,
        )
    }

    if nil == provider {
        return exception.NewError(
            "the provider is required to register a service",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    wrappedProvider, serviceType, reflectedProviderErr := reflectedProvider(serviceName, provider)
    if nil != reflectedProviderErr {
        return reflectedProviderErr
    }

    return instance.register(
        serviceName,
        serviceType,
        wrappedProvider,
        options...,
    )
}

func (instance *container) MustRegister(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    registerErr := instance.Register(serviceName, provider, options...)
    if nil != registerErr {
        exception.Panic(exception.FromError(registerErr))
    }
}
