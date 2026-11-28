package audit

import (
    "context"
    "testing"
)

func TestActorFromContext_RoundTripsTheActorTheCallerSet(t *testing.T) {
    ctx := WithActor(context.Background(), "user-42")

    if "user-42" != ActorFromContext(ctx) {
        t.Fatalf("expected the actor to be carried, got %q", ActorFromContext(ctx))
    }

    if "user-99" != ActorFromContext(WithActor(ctx, "user-99")) {
        t.Fatalf("expected the inner actor to win, got %q", ActorFromContext(WithActor(ctx, "user-99")))
    }
}

func TestActorFromContext_AnswersEmptyWhenNobodyWasNamed(t *testing.T) {
    if "" != ActorFromContext(context.Background()) {
        t.Fatalf("expected no actor on a bare context, got %q", ActorFromContext(context.Background()))
    }

    if "" != ActorFromContext(WithActor(context.Background(), "")) {
        t.Fatalf("expected an empty actor to read back empty, got %q", ActorFromContext(WithActor(context.Background(), "")))
    }
}

func TestActorFromContext_IsBlindToAValueStoredUnderAnotherKey(t *testing.T) {
    type foreignActorKey struct{}

    ctx := context.WithValue(context.Background(), foreignActorKey{}, "user-42")

    if "" != ActorFromContext(ctx) {
        t.Fatalf("expected a foreign key to be invisible, got %q", ActorFromContext(ctx))
    }
}
