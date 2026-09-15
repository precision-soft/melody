package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

/* RegisterScoped registers a service for one scope and requires ScopedRegistrar. The distinct registrar interface prevents container-lifetime registration through this entry point. */
func RegisterScoped[T any](
    registrar containercontract.ScopedRegistrar,
    serviceName string,
    provider containercontract.Provider[T],
    options ...containercontract.RegisterOption,
) error {
    if nil == registrar {
        return exception.NewError(
            "scoped registrar is nil",
            nil,
            nil,
        )
    }

    registerOption := applyRegisterServiceOptions(options)

    serviceType := reflect.TypeOf((*T)(nil)).Elem()
    if true == registerOption.AlsoRegisterType && true == isAnyType(serviceType) {
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
        options...,
    )
}

func MustRegisterScoped[T any](
    registrar containercontract.ScopedRegistrar,
    serviceName string,
    provider containercontract.Provider[T],
    options ...containercontract.RegisterOption,
) {
    registerScopedErr := RegisterScoped[T](registrar, serviceName, provider, options...)
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
    options ...containercontract.RegisterOption,
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

    optionsWithType := append(
        []containercontract.RegisterOption{
            WithTypeRegistration(true),
        },
        options...,
    )

    return RegisterScoped[T](registrar, serviceName, provider, optionsWithType...)
}

func MustRegisterScopedType[T any](
    registrar containercontract.ScopedRegistrar,
    provider containercontract.Provider[T],
    options ...containercontract.RegisterOption,
) {
    registerScopedTypeErr := RegisterScopedType[T](registrar, provider, options...)
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
