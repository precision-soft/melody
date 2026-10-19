package audit

import (
    "context"
    "testing"
)

/* the actor travels on the CONTEXT because it is the request's, not the recorder's: every trail row is written from whatever context the caller's transaction runs under, and there is no other way to name who acted */
func TestActorFromContext_RoundTripsTheActorTheCallerSet(t *testing.T) {
    ctx := WithActor(context.Background(), "user-42")

    if "user-42" != ActorFromContext(ctx) {
        t.Fatalf("expected the actor to be carried, got %q", ActorFromContext(ctx))
    }

    /* the innermost binding wins, which is what lets an impersonating layer name itself over the one below */
    if "user-99" != ActorFromContext(WithActor(ctx, "user-99")) {
        t.Fatalf("expected the inner actor to win, got %q", ActorFromContext(WithActor(ctx, "user-99")))
    }
}

/* an unnamed actor is the ordinary case — a background job, a migration — so the reader answers the empty string rather than refusing, and every trail row simply carries no actor */
func TestActorFromContext_AnswersEmptyWhenNobodyWasNamed(t *testing.T) {
    if "" != ActorFromContext(context.Background()) {
        t.Fatalf("expected no actor on a bare context, got %q", ActorFromContext(context.Background()))
    }

    if "" != ActorFromContext(WithActor(context.Background(), "")) {
        t.Fatalf("expected an empty actor to read back empty, got %q", ActorFromContext(WithActor(context.Background(), "")))
    }
}

/* the key is a private zero-size struct type, so a value another package stored under its own key — even one spelled the same — cannot be read back as the actor */
func TestActorFromContext_IsBlindToAValueStoredUnderAnotherKey(t *testing.T) {
    type foreignActorKey struct{}

    ctx := context.WithValue(context.Background(), foreignActorKey{}, "user-42")

    if "" != ActorFromContext(ctx) {
        t.Fatalf("expected a foreign key to be invisible, got %q", ActorFromContext(ctx))
    }
}
