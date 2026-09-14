package container

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

/* refusalStageOf reads which of a pair of closed-lifetime checks answered. Both registerOnScope and OverrideProtectedInstanceWithOptions guard twice — once at entry and once after the lock hand-off — with refusals that are otherwise identical, so without the stage a test written for the second window passes when the first one answered, which is exactly the run where the window was missed. */
func refusalStageOf(t *testing.T, err error) string {
    t.Helper()

    melodyErr, isMelodyErr := err.(*exception.Error)
    if false == isMelodyErr {
        t.Fatalf("expected a melody error carrying the refusal stage, got %T", err)
    }

    stage, _ := melodyErr.Context()["refusedAt"].(string)

    return stage
}


type contextOnlyCleanup struct { calls int; seen context.Context; failure error }
func (instance *contextOnlyCleanup) CloseWithContext(ctx context.Context) error { instance.calls++; instance.seen = ctx; return instance.failure }
