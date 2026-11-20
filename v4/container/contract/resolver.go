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

    /* Runtime answers the runtime of the resolution in flight. Inside a provider it is the kind of the scope the call that started the creation entered through, over the context that call waits under, and a scope that resolves through the provider's chain, so a cycle closed through the runtime is refused as circular: a scoped provider's hands its registrations, overrides and close to the real scope, a container-owned provider's is suspended, resolving what the container holds and refusing a registration, an override and a close. The answer is a snapshot of the resolution; a provider keeps the resolver, not its runtime. A singleton reads the runtime of its first creator, never a waiter's. Where no resolution is in flight — the container, a scope, a resolver retained after its provider returned, a goroutine view — it answers a detached runtime over context.Background(), never nil; a Lazy that creates its service after the provider returned therefore creates it detached. */
    Runtime() Runtime
}
