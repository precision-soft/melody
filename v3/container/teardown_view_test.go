package container

import (
    "errors"
    "reflect"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* the view is the plan the teardown would run, read without running it: the dependencies of a node are listed sorted, and its wave is the one the drain gives it. */
func TestContainer_TeardownPlan_ListsEachNodeWithItsSortedDependenciesAndItsWave(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if registerErr := serviceContainer.Register(
        "app.declaring",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        WithTeardownDependency("app.storage", "app.holder"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.declaring"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    planned, carriesPlan := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    })
    if false == carriesPlan {
        t.Fatalf("expected the container to carry the teardown plan door")
    }

    entries := planned.TeardownPlan()

    var declaring *containercontract.TeardownPlanEntry
    for index := range entries {
        if "service:app.declaring" == entries[index].NodeKey {
            declaring = &entries[index]
        }
    }

    if nil == declaring {
        t.Fatalf("expected the declaring service in the plan, got %v", entries)
    }

    if 2 != len(declaring.Dependencies) || "service:app.holder" != declaring.Dependencies[0] || "service:app.storage" != declaring.Dependencies[1] {
        t.Fatalf("expected the two declared dependencies sorted, got %v", declaring.Dependencies)
    }

    if 0 != declaring.WaveIndex {
        t.Fatalf("expected the node nothing depends on in wave zero, got %d", declaring.WaveIndex)
    }
}

func TestContainer_TeardownRunsInWaves_ReportsTheArming(t *testing.T) {
    serviceContainer := NewContainer()

    viewed, carriesView := serviceContainer.(interface{ TeardownRunsInWaves() bool })
    if false == carriesView {
        t.Fatalf("expected the container to carry the waves view")
    }

    if true == viewed.TeardownRunsInWaves() {
        t.Fatalf("expected the default teardown to be sequential")
    }

    armParallelTeardown(t, serviceContainer)

    if false == viewed.TeardownRunsInWaves() {
        t.Fatalf("expected the armed teardown to report its waves")
    }
}

type selfHoldingService struct {
    self  *selfHoldingService
    label string
}

func (instance *selfHoldingService) Close() error {
    return nil
}

/* a service filed under its name AND its type is one node, and what it holds of itself is not an edge: the identity the walk records for the service used to be keyed on whichever of the two filings the map handed out last, so the skip of a node's own identity missed on the other filing and the plan listed the service closed before itself — an ordering "proved" over one node, in seven runs out of eight. The check is asked in the canonical key space now, where both filings are the same node. */
func TestContainer_TeardownPlan_AServiceFiledUnderItsNameAndTypeHoldingItselfListsNoDependency(t *testing.T) {
    for round := 0; round < 8; round = round + 1 {
        serviceContainer := NewContainer()

        armParallelTeardown(t, serviceContainer)

        if registerErr := serviceContainer.Register(
            "alias.self",
            func(_ containercontract.Resolver) (*selfHoldingService, error) {
                value := &selfHoldingService{label: "self"}
                value.self = value

                return value, nil
            },
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.GetByType(reflect.TypeOf((*selfHoldingService)(nil))); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        entries := serviceContainer.(interface {
            TeardownPlan() []containercontract.TeardownPlanEntry
        }).TeardownPlan()

        if 1 != len(entries) {
            t.Fatalf("round %d: expected the two filings collapsed onto one node, got %v", round, entries)
        }

        if 0 != len(entries[0].Dependencies) {
            t.Fatalf("round %d: expected a service holding itself to be ordered against nothing, got %v", round, entries[0].Dependencies)
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("round %d: unexpected close error: %v", round, closeErr)
        }
    }
}

/* arming a closed container is refused with the cause Register answers on the same condition; accepted, it walked the closed instances, re-created the records the teardown had released, and left the flag set on a container that will never close again. */
func TestContainer_ArmParallelTeardown_RefusesAClosedContainer(t *testing.T) {
    serviceContainer := NewContainer()

    if registerErr := serviceContainer.Register(
        "app.service",
        func(_ containercontract.Resolver) (*selfHoldingService, error) { return &selfHoldingService{label: "a"}, nil },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.service"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    armErr := serviceContainer.(interface{ ArmParallelTeardown() error }).ArmParallelTeardown()
    if false == errors.Is(armErr, ErrContainerClosed) {
        t.Fatalf("expected arming a closed container to be refused as closed, got %v", armErr)
    }

    concrete := serviceContainer.(*container)

    if true == concrete.teardownInWaves {
        t.Fatal("expected the refused arming to leave the teardown sequential")
    }

    if nil != concrete.heldIdentitiesByNodeKey {
        t.Fatal("expected the refused arming to leave the released records released")
    }
}
