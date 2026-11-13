package contract

import "context"

/* ResolutionContext is the optional capability of a resolver that carries the context of the resolution in flight: the resolver a provider receives answers, while its provider runs, the context of the call that started its creation. The container and a scope do not implement it, so container.ContextOf answers context.Background() for them. */
type ResolutionContext interface {
    Context() context.Context
}
