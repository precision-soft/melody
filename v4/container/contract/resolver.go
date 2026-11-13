package contract

import (
    "context"
    "reflect"
)

/* Resolver resolves services. The doors that can wait on a creation another goroutine runs take the caller's context first: the wait ends when it is cancelled. */
type Resolver interface {
    Get(ctx context.Context, serviceName string) (any, error)

    MustGet(ctx context.Context, serviceName string) any

    GetByType(ctx context.Context, targetType reflect.Type) (any, error)

    MustGetByType(ctx context.Context, targetType reflect.Type) any

    Has(serviceName string) bool

    HasType(targetType reflect.Type) bool
}
