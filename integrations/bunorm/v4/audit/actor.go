package audit

import (
    "context"
)

type actorContextKey struct{}

func WithActor(ctx context.Context, actor string) context.Context {
    return context.WithValue(ctx, actorContextKey{}, actor)
}

func ActorFromContext(ctx context.Context) string {
    actor, _ := ctx.Value(actorContextKey{}).(string)
    return actor
}
