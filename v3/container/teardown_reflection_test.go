package container

import (
    "context"
    "fmt"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestContainer_Close_ArmedAHeldCollaboratorIsClosedAfterTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "holder" != closeSequence[0] || "b" != closeSequence[1] {
        t.Fatalf("expected the held collaborator to be closed after its holder, got %v", closeSequence)
    }
}

func TestContainer_ArmParallelTeardown_AHeldCollaboratorIsOneWavePastTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    planned, carriesPlan := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    })
    if false == carriesPlan {
        t.Fatalf("expected the container to carry the teardown plan door")
    }

    waveByNode := make(map[string]int)
    for _, entry := range planned.TeardownPlan() {
        waveByNode[entry.NodeKey] = entry.WaveIndex
    }

    holderWave, holderPlanned := waveByNode["service:app.holder"]
    storageWave, storagePlanned := waveByNode["service:app.storage"]

    if false == holderPlanned || false == storagePlanned {
        t.Fatalf("expected both services in the teardown plan, got %v", waveByNode)
    }

    if holderWave >= storageWave {
        t.Fatalf("expected the held collaborator at least one wave past its holder, got holder=%d storage=%d", holderWave, storageWave)
    }
}

func TestContainer_Close_NotArmedAHeldCollaboratorIsClosedBeforeTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if "b" != closeSequence[0] || "holder" != closeSequence[1] {
        t.Fatalf("expected the unarmed teardown to keep the creation order, got %v", closeSequence)
    }
}

func TestHeldPointerIdentities_DoesNotEnterAMap(t *testing.T) {
    held := &closeOrderServiceB{}

    throughField := heldPointerIdentities(&capturingHolder{held: held})
    if 0 == len(throughField) {
        t.Fatalf("expected the walk to reach a collaborator held in a field")
    }

    heldIdentity, hasPointer := pointerKeyOf(held)
    if false == hasPointer {
        t.Fatalf("expected the collaborator to carry a pointer identity")
    }

    if false == holdsIdentity(throughField, heldIdentity) {
        t.Fatalf("expected the field walk to find the collaborator")
    }

    throughMap := heldPointerIdentities(map[string]*closeOrderServiceB{"held": held})
    if 0 != len(throughMap) {
        t.Fatalf("expected a map to be neither entered nor counted, got %d identities", len(throughMap))
    }
}

func TestContainer_Close_ArmedAMutuallyHeldPairGainsNoEdgeAndReportsNoCycle(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    parent := &mutualParentService{recorder: recorder}
    child := &mutualChildService{recorder: recorder, parent: parent}
    parent.child = child

    if registerErr := serviceContainer.Register(
        "app.parent",
        func(_ containercontract.Resolver) (*mutualParentService, error) { return parent, nil },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.child",
        func(_ containercontract.Resolver) (*mutualChildService, error) { return child, nil },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.parent", "app.child")

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("a mutually held pair reported a failure over a teardown that closed both: %v", closeErr)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected both services to close, got %v", closeSequence)
    }
}

func TestHeldPointerIdentities_DoesNotReadAnInterfaceFieldOfForeignMemory(t *testing.T) {
    hub := &foreignHub{backplane: &foreignBackplane{label: "first"}}
    stop := make(chan struct{})
    var writes sync.WaitGroup

    writes.Add(1)
    go func() {
        defer writes.Done()

        for label := 0; ; label = label + 1 {
            select {
            case <-stop:
                return
            default:
                hub.swap(&foreignBackplane{label: fmt.Sprint(label)})
            }
        }
    }()

    hubIdentity, _ := pointerKeyOf(hub)

    for iteration := 0; iteration < 200; iteration = iteration + 1 {
        identities := heldPointerIdentities(&hubHolder{hub: hub})

        if false == holdsIdentity(identities, hubIdentity) {
            t.Fatalf("expected the hub itself to be found through the holder's own pointer field")
        }

        for _, held := range identities {
            if reflect.TypeOf((*foreignBackplane)(nil)) == held.identity.valueType {
                t.Fatalf("expected the backplane behind the hub's interface field to stay unread")
            }
        }
    }

    close(stop)
    writes.Wait()
}

func TestHeldPointerIdentities_DoesNotReadInsideAContextItHolds(t *testing.T) {
    for iteration := 0; iteration < 500; iteration = iteration + 1 {
        ctx, cancel := context.WithCancel(context.Background())

        var firstUse sync.WaitGroup
        firstUse.Add(1)

        go func() {
            defer firstUse.Done()

            _ = ctx.Done()
        }()

        identities := heldPointerIdentities(&hubHolder{ctx: ctx})

        firstUse.Wait()
        cancel()

        if 1 != len(identities) || reflect.TypeOf((*hubHolder)(nil)) != identities[0].identity.valueType {
            t.Fatalf("expected the holder alone, with nothing of the context it holds through an interface, got %d identities", len(identities))
        }

        for _, held := range identities {
            if true == strings.HasPrefix(held.identity.valueType.String(), "*context.") {
                t.Fatalf("expected nothing of the context to be read, got %s", held.identity.valueType)
            }
        }
    }
}

func TestHeldPointerIdentities_StillFindsAServiceBehindAnIntermediateStruct(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(heldPointerIdentities(&outerHolder{middle: &intermediateStruct{service: held}}), heldIdentity) {
        t.Fatalf("expected the service behind an intermediate struct pointer to be found")
    }
}

func TestHeldPointerIdentities_FindsAServiceAtTheDepthLimitWhicheverFieldComesFirst(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    for length := 3; length <= 7; length = length + 1 {
        tail := &chainLink{held: held}

        if false == holdsIdentity(heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(length, tail), direct: tail}), heldIdentity) {
            t.Fatalf("expected the service to be found with the long path listed first, chain of %d links", length)
        }

        if false == holdsIdentity(heldPointerIdentities(&directFirstRoot{chain: chainOfLinks(length, tail), direct: tail}), heldIdentity) {
            t.Fatalf("expected the service to be found with the short path listed first, chain of %d links", length)
        }
    }
}

func TestHeldPointerIdentities_StopsAtTheDepthLimit(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(4, &chainLink{held: held})}), heldIdentity) {
        t.Fatalf("expected a service at the depth limit to be found")
    }

    if true == holdsIdentity(heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(5, &chainLink{held: held})}), heldIdentity) {
        t.Fatalf("expected a service past the depth limit not to be found")
    }
}

func TestHeldPointerIdentities_DoesNotReadAPointerOnePastTheDepthLimit(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(heldPointerIdentities(&oddDepthRoot{chain: oddDepthChain(3, held)}), heldIdentity) {
        t.Fatalf("expected a service at depth eleven to be found")
    }

    if true == holdsIdentity(heldPointerIdentities(&oddDepthRoot{chain: oddDepthChain(4, held)}), heldIdentity) {
        t.Fatalf("expected a service one past the depth limit not to be found")
    }
}

func TestHeldPointerIdentities_StopsAtTheNodeBudget(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    last := &wideHolder{}
    last.cells[255][255].service = held

    if true == holdsIdentity(heldPointerIdentities(last), heldIdentity) {
        t.Fatalf("expected the walk to stop at its budget before the last cell of a table wider than it")
    }

    beside := &wideHolderWithPeer{peer: held}

    if false == holdsIdentity(heldPointerIdentities(beside), heldIdentity) {
        t.Fatalf("expected a peer declared after a table wider than the budget to be found before the table is paid for")
    }

    bounded := &boundedHolder{}
    bounded.cells[63][255].service = held

    if false == holdsIdentity(heldPointerIdentities(bounded), heldIdentity) {
        t.Fatalf("expected the last cell of a table within the budget to be reached")
    }
}

func TestContainer_ArmParallelTeardown_WalksAValueFiledUnderNameAndTypeOnce(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if _, getErr := serviceContainer.GetByType(reflect.TypeOf((*capturingHolder)(nil))); nil != getErr {
        t.Fatalf("unexpected get by type error: %v", getErr)
    }

    armParallelTeardown(t, serviceContainer)

    concrete := serviceContainer.(*container)

    concrete.mutex.RLock()
    byName := concrete.heldIdentitiesByNodeKey[containerNameNodeKey("app.holder")]
    byType := concrete.heldIdentitiesByNodeKey[containerTypeNodeKey(canonicalServiceType(reflect.TypeOf((*capturingHolder)(nil))))]
    concrete.mutex.RUnlock()

    if 0 == len(byName) || 0 == len(byType) {
        t.Fatalf("expected the holder to be recorded under its name and its type, got %d and %d", len(byName), len(byType))
    }

    if reflect.ValueOf(byName).Pointer() != reflect.ValueOf(byType).Pointer() {
        t.Fatalf("expected the name and the type node to share one walk's record")
    }
}

func TestHeldPointerIdentities_KeepsAHeldCollaboratorAlive(t *testing.T) {
    finalized := make(chan struct{}, 1)

    holder := &capturingHolder{held: &closeOrderServiceB{}}
    runtime.SetFinalizer(holder.held, func(*closeOrderServiceB) { finalized <- struct{}{} })

    identities := heldPointerIdentities(holder)
    holder.held = nil

    for cycle := 0; cycle < 3; cycle = cycle + 1 {
        runtime.GC()
    }

    select {
    case <-finalized:
        t.Fatalf("expected the record to keep the dropped collaborator alive")
    case <-time.After(50 * time.Millisecond):
    }

    if 0 == len(identities) {
        t.Fatalf("expected the record to hold the collaborator")
    }

    runtime.KeepAlive(identities)

    identities = nil

    for cycle := 0; cycle < 5; cycle = cycle + 1 {
        runtime.GC()

        select {
        case <-finalized:
            return
        case <-time.After(20 * time.Millisecond):
        }
    }

    t.Fatalf("expected the collaborator to be collectable once the record was dropped")
}

func TestContainer_Close_ArmedARingOfHeldCollaboratorsLongerThanTheWalkGainsNoCycle(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 8)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    const ringSize = 8

    nodes := make([]*ringNode, ringSize)
    for index := range nodes {
        nodes[index] = &ringNode{recorder: recorder, label: fmt.Sprintf("ring.%d", index)}
    }

    for index := range nodes {
        nodes[index].next = nodes[(index+1)%ringSize]
    }

    for index, node := range nodes {
        captured := node

        if registerErr := serviceContainer.Register(
            captured.label,
            func(_ containercontract.Resolver) (*ringNode, error) { return captured, nil },
            WithoutTypeRegistration(),
        ); nil != registerErr {
            t.Fatalf("unexpected register error at %d: %v", index, registerErr)
        }
    }

    for _, node := range nodes {
        if _, getErr := serviceContainer.Get(node.label); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("a ring of held collaborators reported a failure over a teardown that closed every node: %v", closeErr)
    }

    if ringSize != len(closeSequence) {
        t.Fatalf("expected every node of the ring to close, got %v", closeSequence)
    }
}

func TestContainer_Close_ArmedAHeldEdgeAgainstAResolvedEdgeIsNotWritten(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    holder := &resolvingHolder{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.collaborator",
        func(_ containercontract.Resolver) (*backHolder, error) { return &backHolder{recorder: recorder, back: holder}, nil },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*resolvingHolder, error) {
            if _, getErr := FromResolver[*backHolder](resolver, "app.collaborator"); nil != getErr {
                return nil, getErr
            }

            return holder, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := FromResolver[*resolvingHolder](serviceContainer, "app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("a held edge against a resolved one reported a failure: %v", closeErr)
    }

    if 2 != len(closeSequence) || "holder" != closeSequence[0] || "collaborator" != closeSequence[1] {
        t.Fatalf("expected the resolved order to hold, got %v", closeSequence)
    }
}

func TestHeldPointerIdentities_DoesNotReadASliceEvenInItsOwnMemory(t *testing.T) {
    own := &closeOrderServiceB{}
    ownIdentity, _ := pointerKeyOf(own)

    foreign := &closeOrderServiceB{}
    foreignIdentity, _ := pointerKeyOf(foreign)

    identities := heldPointerIdentities(&bagHolder{bag: &foreignBag{items: []*closeOrderServiceB{foreign}}, items: []*closeOrderServiceB{own}})

    if true == holdsIdentity(identities, ownIdentity) {
        t.Fatalf("expected a service in the holder's own slice to stay unread, since the holder may have been handed that slice")
    }

    if true == holdsIdentity(identities, foreignIdentity) {
        t.Fatalf("expected a slice inside foreign memory to stay unread")
    }
}

func TestHeldPointerIdentities_DoesNotReadASliceHandedToTheValueByAnotherOwner(t *testing.T) {
    registry := &rewritingRegistry{items: []*closeOrderServiceB{{}}}

    stop := make(chan struct{})
    var writes sync.WaitGroup

    writes.Add(1)
    go func() {
        defer writes.Done()

        for {
            select {
            case <-stop:
                return
            default:
                registry.Rewrite(&closeOrderServiceB{})
            }
        }
    }()

    for round := 0; round < 200; round = round + 1 {
        serviceContainer := NewContainer()

        armParallelTeardown(t, serviceContainer)

        if registerErr := serviceContainer.Register(
            "app.holder",
            func(_ containercontract.Resolver) (*handedSliceHolder, error) { return &handedSliceHolder{items: registry.All()}, nil },
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.Get("app.holder"); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("unexpected close error: %v", closeErr)
        }
    }

    close(stop)
    writes.Wait()
}

func TestContainer_Close_ArmedAHeldEdgeBesideAMutualPairIsStillWritten(t *testing.T) {
    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    a := &mutualPairA{recorder: recorder}
    b := &mutualPairB{recorder: recorder, other: a}
    a.other = b
    c := &pairMemberHolder{a: a, recorder: recorder}

    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    serviceContainer.MustRegister("pair.a", func(_ containercontract.Resolver) (*mutualPairA, error) { return a, nil })
    serviceContainer.MustRegister("pair.holder", func(_ containercontract.Resolver) (*pairMemberHolder, error) { return c, nil })
    serviceContainer.MustRegister(
        "pair.b",
        func(resolver containercontract.Resolver) (*mutualPairB, error) {
            if _, resolveErr := resolver.Get("pair.holder"); nil != resolveErr {
                return nil, resolveErr
            }

            return b, nil
        },
    )

    MustFromResolver[*mutualPairB](serviceContainer, "pair.b")
    MustFromResolver[*mutualPairA](serviceContainer, "pair.a")

    waveOf := make(map[string]int, 3)
    for _, entry := range serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan() {
        waveOf[entry.NodeKey] = entry.WaveIndex
    }

    if waveOf["service:pair.holder"] >= waveOf["service:pair.a"] {
        t.Fatalf("expected the holder in a wave before the member it holds, got %v", waveOf)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    indexOf := func(label string) int {
        for index, value := range closeSequence {
            if label == value {
                return index
            }
        }

        return -1
    }

    if 3 != len(closeSequence) || indexOf("c") > indexOf("a") {
        t.Fatalf("expected the holder closed before the member it holds, got %v", closeSequence)
    }
}

func TestContainer_ArmParallelTeardown_WalksAlreadyBuiltServicesAsPublishedMemory(t *testing.T) {
    serviceContainer := NewContainer()

    hub := &foreignHub{backplane: &foreignBackplane{label: "first"}}

    if registerErr := serviceContainer.Register(
        "app.hub",
        func(_ containercontract.Resolver) (*foreignHub, error) { return hub, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.hub"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    stop := make(chan struct{})
    var writes sync.WaitGroup

    writes.Add(1)
    go func() {
        defer writes.Done()

        for label := 0; ; label = label + 1 {
            select {
            case <-stop:
                return
            default:
                hub.swap(&foreignBackplane{label: fmt.Sprint(label)})
            }
        }
    }()

    armParallelTeardown(t, serviceContainer)

    for iteration := 0; iteration < 200; iteration = iteration + 1 {
        heldPointerIdentities(hub)
    }

    close(stop)
    writes.Wait()

    for _, held := range serviceContainer.(*container).heldIdentitiesByNodeKey[containerNameNodeKey("app.hub")] {
        if reflect.TypeOf((*foreignBackplane)(nil)) == held.identity.valueType {
            t.Fatalf("expected the backplane behind a built service's interface field to stay unread at arming")
        }
    }
}

func TestContainer_Close_ArmedAHeldEdgeAgainstAResolvedEdgeThroughATypeAliasIsNotWritten(t *testing.T) {
    for round := 0; round < 50; round = round + 1 {
        serviceContainer := NewContainer()

        armParallelTeardown(t, serviceContainer)

        var mutex sync.Mutex
        closeSequence := make([]string, 0, 2)
        recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

        resolving := &aliasedResolvingService{recorder: recorder}

        if registerErr := serviceContainer.Register(
            "app.resolving",
            func(resolver containercontract.Resolver) (*aliasedResolvingService, error) {
                if _, resolveErr := FromResolver[*aliasedCapturingService](resolver, "app.capturing"); nil != resolveErr {
                    return nil, resolveErr
                }

                return resolving, nil
            },
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if registerErr := serviceContainer.Register(
            "app.capturing",
            func(_ containercontract.Resolver) (*aliasedCapturingService, error) {
                return &aliasedCapturingService{recorder: recorder, back: resolving}, nil
            },
            WithoutTypeRegistration(),
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.GetByType(reflect.TypeOf((*aliasedResolvingService)(nil))); nil != getErr {
            t.Fatalf("unexpected resolution by type error: %v", getErr)
        }

        if closeErr := serviceContainer.Close(); nil != closeErr {
            t.Fatalf("round %d: a held edge against a resolved edge, one alias away, was written and reported as a cycle: %v", round, closeErr)
        }

        if 2 != len(closeSequence) {
            t.Fatalf("round %d: expected both services to close, got %v", round, closeSequence)
        }
    }
}



func TestContainer_TeardownPlan_DoesNotWriteAnInferredEdgeIntoTheGraph(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    var lateResolver containercontract.Resolver
    held := &closeOrderServiceB{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.held",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            lateResolver = resolver

            return held, nil
        },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return &capturingHolder{recorder: recorder, held: held}, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.held", "app.holder")

    serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    if _, getErr := lateResolver.Get("app.holder"); nil != getErr {
        t.Fatalf("unexpected late resolution error: %v", getErr)
    }

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("an inference the view had written into the graph closed a ring with a later resolution: %v", closeErr)
    }

    if 2 != len(closeSequence) || "b" != closeSequence[0] || "holder" != closeSequence[1] {
        t.Fatalf("expected the resolved order to hold over the inference, got %v", closeSequence)
    }
}

func TestContainer_ArmParallelTeardown_AnUnbuiltDeclaredDependencyIsNotAWayBack(t *testing.T) {
    serviceContainer := NewContainer()

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    held := &closeOrderServiceB{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.held",
        func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return held, nil },
        WithoutTypeRegistration(),
        WithTeardownDependency("app.unbuilt"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.unbuilt",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        WithoutTypeRegistration(),
        WithTeardownDependency("app.holder"),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return &capturingHolder{recorder: recorder, held: held}, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    armParallelTeardown(t, serviceContainer)

    buildEveryRegisteredService(t, serviceContainer, "app.held", "app.holder")

    entries := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    heldPlanned := false

    for _, entry := range entries {
        if "service:app.held" != entry.NodeKey {
            continue
        }

        heldPlanned = true

        if 1 != entry.WaveIndex {
            t.Fatalf("expected the held service one wave past its holder, got %+v", entries)
        }
    }

    if false == heldPlanned {
        t.Fatalf("expected the held service in the plan, got %+v", entries)
    }
}

func TestHeldPointerIdentities_DoesNotWalkAWholeSubtreeAgainFromAShallowerPath(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)
    hub := &fatHub{}

    if false == holdsIdentity(heldPointerIdentities(&collaboratorFirstHolder{collaborator: newCollaboratorChain(held, 4), hub: hub, chain: newHubChain(hub, 2)}), heldIdentity) {
        t.Fatalf("expected the collaborator declared first to be found")
    }

    if false == holdsIdentity(heldPointerIdentities(&chainFirstHolder{chain: newHubChain(hub, 2), hub: hub, collaborator: newCollaboratorChain(held, 4)}), heldIdentity) {
        t.Fatalf("expected the collaborator declared after a chain of links to one large object to be found as well")
    }
}

func TestHeldPointerIdentities_CountsAnArrayOfScalarsAsOneNode(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(heldPointerIdentities(&peerFirstCodec{peer: newCollaboratorChain(held, 1)}), heldIdentity) {
        t.Fatalf("expected the peer declared before the table to be found")
    }

    if false == holdsIdentity(heldPointerIdentities(&tableFirstCodec{peer: newCollaboratorChain(held, 1)}), heldIdentity) {
        t.Fatalf("expected the peer declared after the table to be found as well")
    }
}

func TestHeldPointerIdentities_CountsAStructOfScalarsAsOneNode(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    rootType := reflect.StructOf([]reflect.StructField{
        {Name: "Table", Type: structOfScalarsAsWideAsTheBudget()},
        {Name: "Peer", Type: reflect.TypeOf((*collaboratorLink)(nil))},
    })

    root := reflect.New(rootType)
    root.Elem().Field(1).Set(reflect.ValueOf(newCollaboratorChain(held, 1)))

    if false == holdsIdentity(heldPointerIdentities(root.Interface()), heldIdentity) {
        t.Fatalf("expected the peer one link below the struct of scalars to be found: the struct is one node, not one per field")
    }
}

func TestHeldPointerIdentities_ChargesTheBudgetWhenAnItemIsQueued(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)
    table := &cellTable{peer: newCollaboratorChain(held, 1)}

    runtime.GC()

    var before runtime.MemStats
    runtime.ReadMemStats(&before)

    found := holdsIdentity(heldPointerIdentities(table), heldIdentity)

    var after runtime.MemStats
    runtime.ReadMemStats(&after)

    runtime.KeepAlive(table)

    allocated := after.TotalAlloc - before.TotalAlloc
    if 16<<20 < allocated {
        t.Fatalf("expected the walk over a million pointer-holding cells to queue no more than the budget, it allocated %d bytes", allocated)
    }

    if false == found {
        t.Fatalf("expected the peer one link below the table to be found: it is queued at the depth of the table's rows, before any cell is charged")
    }
}

func TestHeldPointerIdentities_FindsAHeldServiceBehindARingFromTheShortPathWhicheverFieldComesFirst(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    chain, tail := newCycleMemberShape(held)

    if false == holdsIdentity(heldPointerIdentities(&tailThenChainHolder{tail: tail, chain: chain}), heldIdentity) {
        t.Fatalf("expected the collaborator found through the short path declared first")
    }

    chain, tail = newCycleMemberShape(held)

    if false == holdsIdentity(heldPointerIdentities(&chainThenTailHolder{chain: chain, tail: tail}), heldIdentity) {
        t.Fatalf("expected the collaborator found through the short path declared after the chain that cut it")
    }
}

func TestHeldPointerIdentities_AHubOnARingIsPaidForOnceAndTheCollaboratorAfterItIsFound(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    hub := &cyclicHub{}
    tail := &cyclicHubLink{hub: hub}
    head := &cyclicHubLink{next: tail, hub: hub}
    hub.back = head

    if false == holdsIdentity(heldPointerIdentities(&chainFirstCyclicHubHolder{chain: head, hub: hub, collaborator: held}), heldIdentity) {
        t.Fatalf("expected the collaborator declared after a cyclic hub walked whole once to be found")
    }
}

func TestHeldPointerIdentities_FindsAHeldServiceBehindARingHeldThroughAForwardEdgeFromTheShortPath(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    chain, holderOfMember := newCrossEdgeShape(held)

    if false == holdsIdentity(heldPointerIdentities(&crossEdgeHolder{chain: chain, short: holderOfMember}), heldIdentity) {
        t.Fatalf("expected the collaborator found through the short path into the holder of a cycle member")
    }
}

func TestHeldPointerIdentities_CountsAnArrayOfWhatItDoesNotReadAsOneNode(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(heldPointerIdentities(&anyTableFirstCodec{peer: newCollaboratorChain(held, 1)}), heldIdentity) {
        t.Fatalf("expected the peer declared after a table of interfaces to be found")
    }

    if false == holdsIdentity(heldPointerIdentities(&sliceTableFirstCodec{peer: newCollaboratorChain(held, 1)}), heldIdentity) {
        t.Fatalf("expected the peer declared after a table of slices to be found")
    }
}

func TestHeldPointerIdentities_ReadsAValuePointingAtItselfOnce(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    holder := &selfReferringHolder{collaborator: held}
    holder.self = holder

    identities := heldPointerIdentities(holder)

    if false == holdsIdentity(identities, heldIdentity) {
        t.Fatalf("expected the collaborator of a self-referring holder to be found")
    }

    if 2 != len(identities) {
        t.Fatalf("expected the holder and its collaborator recorded once each, got %d", len(identities))
    }
}

func TestContainer_Close_ArmedTwoListenersHoldingTheirBusBackStillCloseTogether(t *testing.T) {
    var inFlight atomic.Int32
    var peak atomic.Int32

    serviceContainer := NewContainer()

    for _, name := range []string{"app.p", "app.q"} {
        serviceContainer.MustRegister(
            name,
            func(_ containercontract.Resolver) (*busListener, error) { return &busListener{inFlight: &inFlight, peak: &peak}, nil },
            WithoutTypeRegistration(),
        )
    }

    serviceContainer.MustRegister(
        "app.bus",
        func(resolver containercontract.Resolver) (*listenerBus, error) {
            bus := &listenerBus{}

            for _, name := range []string{"app.p", "app.q"} {
                listener, resolveErr := FromResolver[*busListener](resolver, name)
                if nil != resolveErr {
                    return nil, resolveErr
                }

                listener.back = bus
                bus.listeners = append(bus.listeners, listener)
            }

            return bus, nil
        },
    )

    MustFromResolver[*listenerBus](serviceContainer, "app.bus")

    armParallelTeardown(t, serviceContainer)

    if closeErr := serviceContainer.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != peak.Load() {
        t.Fatalf("expected the two listeners, unrelated to each other, closed together in the wave past their bus, got a peak of %d", peak.Load())
    }
}

func TestHeldPointerIdentities_ReadsAPreBuiltValueAProviderHandsBackAsPublishedMemory(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    hub := &foreignHub{backplane: &foreignBackplane{label: "first"}}

    stop := make(chan struct{})
    var writes sync.WaitGroup

    writes.Add(1)
    go func() {
        defer writes.Done()

        for label := 0; ; label = label + 1 {
            select {
            case <-stop:
                return
            default:
                hub.swap(&foreignBackplane{label: fmt.Sprint(label)})
            }
        }
    }()

    for round := 0; round < 200; round = round + 1 {
        if registerErr := serviceContainer.Register(
            fmt.Sprintf("app.hub.%d", round),
            func(_ containercontract.Resolver) (*foreignHub, error) { return hub, nil },
            WithoutTypeRegistration(),
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.Get(fmt.Sprintf("app.hub.%d", round)); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }
    }

    close(stop)
    writes.Wait()
}

func TestContainer_TeardownPlan_NamesTheSerialGroupOfAMutuallyHeldPair(t *testing.T) {
    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    a := &mutualPairA{recorder: recorder}
    b := &mutualPairB{recorder: recorder, other: a}
    a.other = b

    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    serviceContainer.MustRegister("pair.a", func(_ containercontract.Resolver) (*mutualPairA, error) { return a, nil })
    serviceContainer.MustRegister("pair.b", func(_ containercontract.Resolver) (*mutualPairB, error) { return b, nil })
    serviceContainer.MustRegister("pair.alone", func(_ containercontract.Resolver) (*closeOrderServiceC, error) { return &closeOrderServiceC{recorder: recorder}, nil })

    MustFromResolver[*mutualPairA](serviceContainer, "pair.a")
    MustFromResolver[*mutualPairB](serviceContainer, "pair.b")
    MustFromResolver[*closeOrderServiceC](serviceContainer, "pair.alone")

    groupOf := make(map[string]int, 3)
    for _, entry := range serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan() {
        groupOf[entry.NodeKey] = entry.SerialGroup
    }

    if 0 == groupOf["service:pair.a"] || groupOf["service:pair.a"] != groupOf["service:pair.b"] {
        t.Fatalf("expected the pair in one named group, got %v", groupOf)
    }

    if 0 != groupOf["service:pair.alone"] {
        t.Fatalf("expected the service in no group to read zero, got %v", groupOf)
    }
}
