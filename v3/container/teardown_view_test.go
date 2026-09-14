package container

import (
    "errors"
    "reflect"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
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

    /* the refusal names the door it refused, not a service in creation: the resolver's constructor has one slot, and "creatingKey: ArmParallelTeardown" read as a service of that name */
    if "ArmParallelTeardown" != exception.LogContext(armErr)["door"] {
        t.Fatalf("expected the refusal to name the door, got %v", exception.LogContext(armErr))
    }

    concrete := serviceContainer.(*container)

    if true == concrete.teardownInWaves {
        t.Fatal("expected the refused arming to leave the teardown sequential")
    }

    if nil != concrete.heldIdentitiesByNodeKey {
        t.Fatal("expected the refused arming to leave the released records released")
    }
}

/* the remainder the drain leaves holds the ring and the pure dependencies of its members alike; the flag names the ring: a resolves b, b declares a, and b resolves c — c is closed with the remainder and is on no ring */
func TestTeardownPlan_APureDependencyOfARingMemberIsNotFlaggedAsACycle(t *testing.T) {
    serviceContainer := NewContainer()

    serviceContainer.MustRegister(
        "cycle.c",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "c"}, nil },
        WithoutTypeRegistration(),
    )

    serviceContainer.MustRegister(
        "cycle.b",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get("cycle.c"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "b"}, nil
        },
        WithoutTypeRegistration(),
        WithTeardownDependency("cycle.a"),
    )

    serviceContainer.MustRegister(
        "cycle.a",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get("cycle.b"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "a"}, nil
        },
        WithoutTypeRegistration(),
    )

    MustFromResolver[*poolDeclarerService](serviceContainer, "cycle.a")

    flagged := make(map[string]bool)
    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        flagged[entry.NodeKey] = entry.Cycle
    }

    if false == flagged["service:cycle.a"] || false == flagged["service:cycle.b"] {
        t.Fatalf("expected the two ring members to be flagged, got %v", flagged)
    }

    if true == flagged["service:cycle.c"] {
        t.Fatalf("expected the pure dependency of a ring member not to be flagged as a cycle, got %v", flagged)
    }
}

/* a bridge between two rings — a service one ring member depends on, that depends on a member of the other ring — is on no ring: it is released once the first ring closes and closed before the second, in the order the graph proves, so it is not flagged */
func TestTeardownPlan_ABridgeBetweenTwoRingsIsNotFlaggedAsACycle(t *testing.T) {
    serviceContainer := NewContainer()

    for _, name := range []string{"ring.f", "ring.e", "bridge", "ring.b", "ring.a"} {
        label := name

        serviceContainer.MustRegister(
            name,
            func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: label}, nil },
            WithoutTypeRegistration(),
        )
    }

    for _, name := range []string{"ring.a", "ring.b", "bridge", "ring.e", "ring.f"} {
        MustFromResolver[*poolDeclarerService](serviceContainer, name)
    }

    concrete := serviceContainer.(*container)
    concrete.mutex.Lock()
    concrete.registerDependencyLocked("service:ring.a", "service:ring.b")
    concrete.registerDependencyLocked("service:ring.b", "service:ring.a")
    concrete.registerDependencyLocked("service:ring.a", "service:bridge")
    concrete.registerDependencyLocked("service:bridge", "service:ring.e")
    concrete.registerDependencyLocked("service:ring.e", "service:ring.f")
    concrete.registerDependencyLocked("service:ring.f", "service:ring.e")
    concrete.mutex.Unlock()

    flagged := make(map[string]bool)
    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        flagged[entry.NodeKey] = entry.Cycle
    }

    for _, member := range []string{"service:ring.a", "service:ring.b", "service:ring.e", "service:ring.f"} {
        if false == flagged[member] {
            t.Fatalf("expected %s to be flagged as a ring member, got %v", member, flagged)
        }
    }

    if true == flagged["service:bridge"] {
        t.Fatalf("expected the bridge between the two rings not to be flagged, got %v", flagged)
    }
}
