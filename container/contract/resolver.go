package contract

import "reflect"

type Resolver interface {
	Get(serviceName string) (any, error)

	MustGet(serviceName string) any

	GetByType(targetType reflect.Type) (any, error)

	MustGetByType(targetType reflect.Type) any

	Has(serviceName string) bool

	HasType(targetType reflect.Type) bool
}
