package container

import (
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
