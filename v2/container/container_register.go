package container

import (
	"reflect"

	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/exception"
)

func Register[T any](
	registrar containercontract.Registrar,
	serviceName string,
	provider containercontract.Provider[T],
	options ...containercontract.RegisterOption,
) error {
	if nil == registrar {
		return exception.NewError(
			"registrar is nil",
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

	return registrar.Register(
		serviceName,
		provider,
		options...,
	)
}

func MustRegister[T any](
	registrar containercontract.Registrar,
	serviceName string,
	provider containercontract.Provider[T],
	options ...containercontract.RegisterOption,
) {
	registerErr := Register[T](registrar, serviceName, provider, options...)
	if nil != registerErr {
		exception.Panic(
			exception.NewError(
				"failed to register service",
				map[string]any{
					"serviceName": serviceName,
					"serviceType": reflect.TypeOf((*T)(nil)).Elem().String(),
				},
				registerErr,
			),
		)
	}
}

func RegisterType[T any](
	registrar containercontract.Registrar,
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

	return Register[T](registrar, serviceName, provider, optionsWithType...)
}

func MustRegisterType[T any](
	registrar containercontract.Registrar,
	provider containercontract.Provider[T],
	options ...containercontract.RegisterOption,
) {
	registerTypeErr := RegisterType[T](registrar, provider, options...)
	if nil != registerTypeErr {
		exception.Panic(
			exception.NewError(
				"failed to register service by type",
				map[string]any{
					"serviceType": reflect.TypeOf((*T)(nil)).Elem().String(),
				},
				registerTypeErr,
			),
		)
	}
}
