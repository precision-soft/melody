package container

import (
    "context"
    "errors"
    "reflect"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* the view is the plan the teardown would run, read without running it: the dependencies of a node are listed sorted, and its wave is the one the drain gives it. */
func TestContainer_TeardownPlan_ListsEachNodeWithItsSortedDependenciesAndItsWave(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if registerErr := serviceContainer.Register(
        "app.declaring",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        RegisterOptions{TeardownDependencyNames: []string{"app.storage", "app.holder"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.declaring"); nil != getErr {
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

func TestContainer_TeardownRunsInWaves_ReportsTheConfiguration(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        config   ContainerConfig
        expected bool
    }{
        {name: "default", config: ContainerConfig{}, expected: true},
        {name: "sequential", config: ContainerConfig{TeardownSequential: true}, expected: false},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            viewed, carriesView := NewContainer(testCase.config).(interface{ TeardownRunsInWaves() bool })
            if false == carriesView {
                t.Fatalf("expected the container to carry the waves view")
            }

            if testCase.expected != viewed.TeardownRunsInWaves() {
                t.Fatalf("expected TeardownRunsInWaves %v, got %v", testCase.expected, viewed.TeardownRunsInWaves())
            }
        })
    }
}

type selfHoldingService struct {
    self  *selfHoldingService
    label string
}

func (instance *selfHoldingService) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestContainer_TeardownPlan_AServiceFiledUnderItsNameAndTypeHoldingItselfListsNoDependency(t *testing.T) {
    for round := 0; round < 8; round = round + 1 {
        serviceContainer := NewContainer(ContainerConfig{})

        verifyDeclaredTeardownDependencies(t, serviceContainer)

        if registerErr := serviceContainer.Register(
            "alias.self",
            func(_ containercontract.Resolver) (*selfHoldingService, error) {
                value := &selfHoldingService{label: "self"}
                value.self = value

                return value, nil
            },
            RegisterOptions{},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.GetByType(t.Context(), reflect.TypeOf((*selfHoldingService)(nil))); nil != getErr {
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

/* the verification of a closed container is refused with the cause Register answers on the same condition. */
func TestContainer_VerifyDeclaredTeardownDependencies_RefusesAClosedContainer(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    if registerErr := serviceContainer.Register(
        "app.service",
        func(_ containercontract.Resolver) (*selfHoldingService, error) { return &selfHoldingService{label: "a"}, nil },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.service"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies()
    if false == errors.Is(verifyErr, ErrContainerClosed) {
        t.Fatalf("expected the verification of a closed container to be refused as closed, got %v", verifyErr)
    }

    /* the refusal names the door it refused, not a service in creation */
    if "VerifyDeclaredTeardownDependencies" != exception.LogContext(verifyErr, exception.LogContextConfig{})["door"] {
        t.Fatalf("expected the refusal to name the door, got %v", exception.LogContext(verifyErr, exception.LogContextConfig{}))
    }
}

/* the remainder the drain leaves holds the ring and the pure dependencies of its members alike; the flag names the ring: a resolves b, b resolves c, and an edge b → a no door admits is written past them — c is closed with the remainder and is on no ring */
func TestTeardownPlan_APureDependencyOfARingMemberIsNotFlaggedAsACycle(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister(
        "cycle.c",
        func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: "c"}, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "cycle.b",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get(ContextOf(resolver), "cycle.c"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "b"}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    serviceContainer.MustRegister(
        "cycle.a",
        func(resolver containercontract.Resolver) (*poolDeclarerService, error) {
            if _, resolveErr := resolver.Get(ContextOf(resolver), "cycle.b"); nil != resolveErr {
                return nil, resolveErr
            }

            return &poolDeclarerService{label: "a"}, nil
        },
        RegisterOptions{TypeRegistrationDisabled: true},
    )

    MustFromResolver[*poolDeclarerService](t.Context(), serviceContainer, "cycle.a")

    writeDependencyRingEdge(t, serviceContainer, "cycle.b", "cycle.a")

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
    serviceContainer := NewContainer(ContainerConfig{})

    for _, name := range []string{"ring.f", "ring.e", "bridge", "ring.b", "ring.a"} {
        label := name

        serviceContainer.MustRegister(
            name,
            func(_ containercontract.Resolver) (*poolDeclarerService, error) { return &poolDeclarerService{label: label}, nil },
            RegisterOptions{TypeRegistrationDisabled: true},
        )
    }

    for _, name := range []string{"ring.a", "ring.b", "bridge", "ring.e", "ring.f"} {
        MustFromResolver[*poolDeclarerService](t.Context(), serviceContainer, name)
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
