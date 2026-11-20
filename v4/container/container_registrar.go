package container

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func (instance *container) Register(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
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

    registerErr := instance.register(
        serviceName,
        serviceType,
        wrappedProvider,
        options,
    )
    if nil == registerErr && true == options.BuiltAtBoot {
        instance.builtAtBootMutex.Lock()
        instance.builtAtBootNames = append(instance.builtAtBootNames, serviceName)
        instance.builtAtBootMutex.Unlock()
    }

    return registerErr
}

/* BuiltAtBootServiceNames answers the names registered with BuiltAtBoot, in registration order. */
func (instance *container) BuiltAtBootServiceNames() []string {
    instance.builtAtBootMutex.Lock()
    defer instance.builtAtBootMutex.Unlock()

    return append([]string(nil), instance.builtAtBootNames...)
}

func (instance *container) MustRegister(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
) {
    registerErr := instance.Register(serviceName, provider, options)
    if nil != registerErr {
        exception.Panic(exception.FromError(registerErr))
    }
}
