package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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

/* armParallelTeardown reaches the opt-in the way an application does: through a type assertion on the concrete container, because the Container contract declares neither this door nor IsClosed nor CloseWithContext, for the reason written at each of them. */
func armParallelTeardown(t *testing.T, serviceContainer containercontract.Container) {
    t.Helper()

    armable, isArmable := serviceContainer.(interface{ ArmParallelTeardown() error })
    if false == isArmable {
        t.Fatalf("expected the container to carry the parallel teardown door")
    }

    if armErr := armable.ArmParallelTeardown(); nil != armErr {
        t.Fatalf("unexpected arm error: %v", armErr)
    }
}

func buildEveryRegisteredService(t *testing.T, serviceContainer containercontract.Container, serviceNames ...string) {
    t.Helper()

    for _, serviceName := range serviceNames {
        if _, getErr := serviceContainer.Get(serviceName); nil != getErr {
            t.Fatalf("unexpected get error for %s: %v", serviceName, getErr)
        }
    }
}
