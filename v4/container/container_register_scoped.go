package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* RegisterScoped is Register for a service whose lifetime is one scope. It takes a ScopedRegistrar only, and the two registrar interfaces share no method, so a registration handed the wrong registrar does not compile. */
func RegisterScoped[T any](
    registrar containercontract.ScopedRegistrar,
    serviceName string,
    provider containercontract.Provider[T],
    options containercontract.RegisterOptions,
) error {
    if nil == registrar {
        return exception.NewError(
            "scoped registrar is nil",
            nil,
            nil,
        )
    }

    serviceType := reflect.TypeOf((*T)(nil)).Elem()
    if false == options.TypeRegistrationDisabled && true == isAnyType(serviceType) {
        return exception.NewError(
            "type registration requires a concrete type",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    return registrar.RegisterScoped(
        serviceName,
        provider,
        options,
    )
}

func MustRegisterScoped[T any](
    registrar containercontract.ScopedRegistrar,
    serviceName string,
    provider containercontract.Provider[T],
    options containercontract.RegisterOptions,
) {
    registerScopedErr := RegisterScoped[T](registrar, serviceName, provider, options)
    if nil != registerScopedErr {
        exception.Panic(
            exception.NewError(
                "failed to register scoped service",
                map[string]any{
                    "serviceName": serviceName,
                    "serviceType": reflect.TypeOf((*T)(nil)).Elem().String(),
                },
                registerScopedErr,
            ),
        )
    }
}

func RegisterScopedType[T any](
    registrar containercontract.ScopedRegistrar,
    provider containercontract.Provider[T],
    options containercontract.RegisterOptions,
) error {
    serviceType := reflect.TypeOf((*T)(nil)).Elem()

    serviceName := defaultServiceNameForType(serviceType)
    if "" == serviceName {
        return exception.NewError(
            "could not determine service name for type",
            map[string]any{
                "serviceType": serviceType.String(),
            },
            nil,
        )
    }

    return RegisterScoped[T](registrar, serviceName, provider, options)
}

func MustRegisterScopedType[T any](
    registrar containercontract.ScopedRegistrar,
    provider containercontract.Provider[T],
    options containercontract.RegisterOptions,
) {
    registerScopedTypeErr := RegisterScopedType[T](registrar, provider, options)
    if nil != registerScopedTypeErr {
        exception.Panic(
            exception.NewError(
                "failed to register scoped service by type",
                map[string]any{
                    "serviceType": reflect.TypeOf((*T)(nil)).Elem().String(),
                },
                registerScopedTypeErr,
            ),
        )
    }
}
