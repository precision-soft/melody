package container

import (
    "context"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* runtimeOver is the runtime a caller holding only a context writes for this handle, runtime.WithContext(resolver.Runtime(), ctx): the source's own kind and scope over that context */
func (instance *LazyService[T]) runtimeOver(ctx context.Context) containercontract.Runtime {
    instance.mutex.Lock()
    source := instance.source
    instance.mutex.Unlock()

    sourceResolver, isResolver := source.(containercontract.Resolver)
    if false == isResolver || nil == sourceResolver {
        return &resolutionRuntime{ctx: ctx, kind: containercontract.KindDetached, scope: &suspendedScope{}}
    }

    sourceRuntime := sourceResolver.Runtime()

    return &resolutionRuntime{ctx: ctx, kind: sourceRuntime.Kind(), scope: sourceRuntime.Scope()}
}

func (instance *LazyService[T]) getOver(ctx context.Context) T {
    return instance.Get(instance.runtimeOver(ctx))
}

func (instance *LazyService[T]) resolveOver(ctx context.Context) (T, error) {
    return instance.Resolve(instance.runtimeOver(ctx))
}
