package container

import (
	"errors"
	"reflect"

	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/exception"
	"github.com/precision-soft/melody/internal"
)

func FromResolver[T any](resolver containercontract.Resolver, serviceName string) (T, error) {
	value, getErr := resolver.Get(serviceName)
	if nil != getErr {
		var zero T

		var melodyErr *exception.Error
		isMelodyErr := errors.As(getErr, &melodyErr)

		if true == isMelodyErr && nil != melodyErr {
			context := melodyErr.Context()
			context["serviceName"] = serviceName

			return zero, exception.NewError(
				melodyErr.Message(),
				context,
				melodyErr.CauseErr(),
			)
		}

		return zero, exception.NewError(
			"service not registered in resolver",
			map[string]any{
				"serviceName": serviceName,
			},
			getErr,
		)
	}

	typedValue, ok := value.(T)
	if false == ok {
		var zero T

		expectedType := reflect.TypeOf((*T)(nil)).Elem()

		return zero, exception.NewError(
			"service has wrong type",
			map[string]any{
				"serviceName":  serviceName,
				"expectedType": expectedType.String(),
				"actualType":   typeString(value),
			},
			nil,
		)
	}

	return typedValue, nil
}

func MustFromResolver[T any](resolver containercontract.Resolver, serviceName string) T {
	typedValue, fromResolverErr := FromResolver[T](resolver, serviceName)
	if nil != fromResolverErr {
		exception.Panic(
			exception.FromError(fromResolverErr),
		)
	}

	if true == internal.IsNilInterface(typedValue) {
		exception.Panic(
			exception.NewError("resolver returned nil value", map[string]any{"serviceName": serviceName}, nil),
		)
	}

	return typedValue
}

func FromResolverByType[T any](resolver containercontract.Resolver) (T, error) {
	targetType := reflect.TypeOf((*T)(nil)).Elem()
	canonicalTargetType := canonicalServiceType(targetType)

	value, getByTypeErr := resolver.GetByType(canonicalTargetType)
	if nil != getByTypeErr {
		var zero T
		return zero, getByTypeErr
	}

	typedValue, ok := value.(T)
	if false == ok {
		var zero T
		return zero, exception.NewError(
			"resolved service has unexpected type",
			map[string]any{
				"expectedType": canonicalTargetType.String(),
				"actualType":   typeString(value),
			},
			nil,
		)
	}

	return typedValue, nil
}

func MustFromResolverByType[T any](resolver containercontract.Resolver) T {
	castValue, fromResolverByTypeErr := FromResolverByType[T](resolver)
	if nil != fromResolverByTypeErr {
		exception.Panic(
			exception.FromError(fromResolverByTypeErr),
		)
	}

	return castValue
}

func typeString(value any) string {
	if nil == value {
		return "<nil>"
	}

	return reflect.TypeOf(value).String()
}
