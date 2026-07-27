package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
)

var (
    resolverInterfaceType = reflect.TypeOf((*containercontract.Resolver)(nil)).Elem()
    errorInterfaceType    = reflect.TypeOf((*error)(nil)).Elem()
)

/* reflectedProvider validates a provider as written by hand and wraps it into the container's own provider shape, re-checking the produced value against the declared service type. It is the one place the provider contract is enforced, so a container registration, a scoped registration and a registration made on a live scope cannot drift apart. It yields the wrapped provider and the service type the signature declares. */
func reflectedProvider(serviceName string, provider any) (providerAny, reflect.Type, error) {
    providerValue := reflect.ValueOf(provider)
    providerType := providerValue.Type()

    validateRegistrarProviderSignatureErr := validateRegistrarProviderSignature(
        serviceName,
        providerType,
    )
    if nil != validateRegistrarProviderSignatureErr {
        return nil, nil, validateRegistrarProviderSignatureErr
    }

    serviceType := providerType.Out(0)

    wrappedProvider := func(resolver containercontract.Resolver) (any, error) {
        results := providerValue.Call(
            []reflect.Value{
                reflect.ValueOf(resolver),
            },
        )

        value := results[0].Interface()

        errorInterface := results[1].Interface()

        var err error
        if nil != errorInterface {
            var ok bool
            err, ok = errorInterface.(error)
            if false == ok {
                return nil, exception.NewError(
                    "provider for service returned a non error second value",
                    map[string]any{
                        "serviceName": serviceName,
                    },
                    nil,
                )
            }
        }

        if nil != value {
            valueType := reflect.TypeOf(value)
            if false == valueType.AssignableTo(serviceType) {
                return nil, exception.NewError(
                    "provider returned a value with unexpected type",
                    map[string]any{
                        "serviceName":  serviceName,
                        "expectedType": serviceType.String(),
                        "actualType":   valueType.String(),
                    },
                    nil,
                )
            }
        }

        return value, err
    }

    return wrappedProvider, serviceType, nil
}

func validateRegistrarProviderSignature(
    serviceName string,
    providerType reflect.Type,
) error {
    if reflect.Func != providerType.Kind() {
        return exception.NewError(
            "provider must be a function",
            map[string]any{
                "serviceName":  serviceName,
                "providerKind": providerType.Kind().String(),
            },
            nil,
        )
    }

    if 1 != providerType.NumIn() {
        return exception.NewError(
            "provider must accept exactly one argument",
            map[string]any{
                "serviceName": serviceName,
                "inputsCount": providerType.NumIn(),
            },
            nil,
        )
    }

    inputType := providerType.In(0)
    if resolverInterfaceType != inputType {
        return exception.NewError(
            "provider first argument must be exactly resolver",
            map[string]any{
                "serviceName":     serviceName,
                "expectedArgType": resolverInterfaceType.String(),
                "actualArgType":   inputType.String(),
            },
            nil,
        )
    }

    if 2 != providerType.NumOut() {
        return exception.NewError(
            "provider must return exactly two values",
            map[string]any{
                "serviceName":  serviceName,
                "outputsCount": providerType.NumOut(),
            },
            nil,
        )
    }

    valueType := providerType.Out(0)

    if true == isEmptyInterfaceType(valueType) {
        return exception.NewError(
            "provider must not return any",
            map[string]any{
                "serviceName": serviceName,
                "serviceType": valueType.String(),
            },
            nil,
        )
    }

    secondReturnType := providerType.Out(1)
    if false == secondReturnType.Implements(errorInterfaceType) {
        return exception.NewError(
            "provider second return value must be error",
            map[string]any{
                "serviceName":        serviceName,
                "expectedSecondType": errorInterfaceType.String(),
                "actualSecondType":   secondReturnType.String(),
            },
            nil,
        )
    }

    return nil
}

func isEmptyInterfaceType(targetType reflect.Type) bool {
    if reflect.Interface != targetType.Kind() {
        return false
    }

    if 0 != targetType.NumMethod() {
        return false
    }

    return true
}
