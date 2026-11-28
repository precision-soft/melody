package container

import (
    "context"
    "fmt"
    "math/rand"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func TestContainer_Close_WavesAHeldCollaboratorIsClosedAfterTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected 2 close calls, got %d: %v", len(closeSequence), closeSequence)
    }

    if "holder" != closeSequence[0] || "b" != closeSequence[1] {
        t.Fatalf("expected the held collaborator to be closed after its holder, got %v", closeSequence)
    }
}

func TestContainer_Waves_AHeldCollaboratorIsOneWavePastTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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

func TestContainer_Close_SequentialAHeldCollaboratorIsClosedBeforeTheServiceHoldingIt(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{TeardownSequential: true})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if "b" != closeSequence[0] || "holder" != closeSequence[1] {
        t.Fatalf("expected the sequential teardown to keep the creation order, got %v", closeSequence)
    }
}

func TestHeldPointerIdentities_DoesNotEnterAMap(t *testing.T) {
    held := &closeOrderServiceB{}

    throughField := walkHeldPointers(&capturingHolder{held: held}, nil, nil)
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

    throughMap := walkHeldPointers(map[string]*closeOrderServiceB{"held": held}, nil, nil)
    if 0 != len(throughMap) {
        t.Fatalf("expected a map to be neither entered nor counted, got %d identities", len(throughMap))
    }
}

func holdsIdentity(identities []heldPointer, wanted pointerIdentity) bool {
    for _, held := range identities {
        if held.identity == wanted {
            return true
        }
    }

    return false
}

type mutualParentService struct {
    recorder *closeOrderRecorder
    child    *mutualChildService
}

func (instance *mutualParentService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("parent")

    return nil
}

type mutualChildService struct {
    recorder *closeOrderRecorder
    parent   *mutualParentService
}

func (instance *mutualChildService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("child")

    return nil
}

func TestContainer_Close_WavesAMutuallyHeldPairGainsNoEdgeAndReportsNoCycle(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    parent := &mutualParentService{recorder: recorder}
    child := &mutualChildService{recorder: recorder, parent: parent}
    parent.child = child

    if registerErr := serviceContainer.Register(
        "app.parent",
        func(_ containercontract.Resolver) (*mutualParentService, error) { return parent, nil },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.child",
        func(_ containercontract.Resolver) (*mutualChildService, error) { return child, nil },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.parent", "app.child")

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("a mutually held pair reported a failure over a teardown that closed both: %v", closeErr)
    }

    if 2 != len(closeSequence) {
        t.Fatalf("expected both services to close, got %v", closeSequence)
    }
}

type foreignHub struct {
    mutex     sync.Mutex
    backplane fmt.Stringer
}

func (instance *foreignHub) swap(backplane fmt.Stringer) {
    instance.mutex.Lock()
    instance.backplane = backplane
    instance.mutex.Unlock()
}

func (instance *foreignHub) CloseWithContext(closeContext context.Context) error { return nil }

type foreignBackplane struct{ label string }

func (instance *foreignBackplane) String() string { return instance.label }

type hubHolder struct {
    hub *foreignHub
    ctx context.Context
}

func (instance *hubHolder) CloseWithContext(closeContext context.Context) error { return nil }

func TestHeldPointerIdentities_SeesACollaboratorHeldThroughAnAtomicPointer(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    collaborator := &atomicallyHeldCollaborator{}

    serviceContainer.MustRegister("app.collaborator", func(_ containercontract.Resolver) (*atomicallyHeldCollaborator, error) { return collaborator, nil }, RegisterOptions{})
    serviceContainer.MustRegister("app.atomic", func(_ containercontract.Resolver) (*atomicPointerHolder, error) {
        holder := &atomicPointerHolder{}
        holder.collaborator.Store(collaborator)

        return holder, nil
    }, RegisterOptions{})
    serviceContainer.MustRegister("app.plain", func(_ containercontract.Resolver) (*plainPointerHolder, error) {
        return &plainPointerHolder{collaborator: collaborator}, nil
    }, RegisterOptions{})

    if verifyErr := serviceContainer.(declaredTeardownVerifier).VerifyDeclaredTeardownDependencies(); nil != verifyErr {
        t.Fatalf("unexpected verify error: %v", verifyErr)
    }

    MustFromResolver[*atomicallyHeldCollaborator](t.Context(), serviceContainer, "app.collaborator")
    MustFromResolver[*atomicPointerHolder](t.Context(), serviceContainer, "app.atomic")
    MustFromResolver[*plainPointerHolder](t.Context(), serviceContainer, "app.plain")

    dependenciesOf := make(map[string][]string)
    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        dependenciesOf[entry.NodeKey] = entry.Dependencies
    }

    if "[service:app.collaborator]" != fmt.Sprint(dependenciesOf["service:app.atomic"]) {
        t.Fatalf("expected the collaborator behind the atomic pointer seen, got %v", dependenciesOf["service:app.atomic"])
    }

    if "[service:app.collaborator]" != fmt.Sprint(dependenciesOf["service:app.plain"]) {
        t.Fatalf("expected the collaborator behind the plain pointer seen, got %v", dependenciesOf["service:app.plain"])
    }
}

type atomicallyHeldCollaborator struct {
    label string
}

func (instance *atomicallyHeldCollaborator) CloseWithContext(closeContext context.Context) error {
    return nil
}

type atomicPointerHolder struct {
    collaborator atomic.Pointer[atomicallyHeldCollaborator]
}

func (instance *atomicPointerHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

type plainPointerHolder struct {
    collaborator *atomicallyHeldCollaborator
}

func (instance *plainPointerHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

type atomicChainLink struct {
    next *atomicChainLink
    held atomic.Pointer[closeOrderServiceB]
}

type atomicChainRoot struct {
    chain *atomicChainLink
}

func atomicChainOfLinks(length int, held *closeOrderServiceB) *atomicChainLink {
    head := &atomicChainLink{}
    head.held.Store(held)

    for count := 0; count < length; count = count + 1 {
        head = &atomicChainLink{next: head}
    }

    return head
}

func TestHeldPointerIdentities_AnAtomicPointerReachesAsDeepAsAPlainPointerField(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(walkHeldPointers(&chainFirstRoot{chain: chainOfLinks(4, &chainLink{held: held})}, nil, nil), heldIdentity) {
        t.Fatalf("expected the plain pointer field at the depth limit to be found")
    }

    if false == holdsIdentity(walkHeldPointers(&atomicChainRoot{chain: atomicChainOfLinks(4, held)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the atomic pointer at the depth limit to be found as the plain pointer field is")
    }

    if true == holdsIdentity(walkHeldPointers(&atomicChainRoot{chain: atomicChainOfLinks(5, held)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the atomic pointer past the depth limit not to be found")
    }
}

func TestHeldPointerIdentities_ANilAtomicPointerHoldsNothing(t *testing.T) {
    found := walkHeldPointers(&atomicPointerHolder{}, nil, nil)

    if 1 != len(found) {
        t.Fatalf("expected only the holder itself, got %d identities", len(found))
    }
}

func TestHeldPointerIdentities_AnAtomicPointerInAValueNotAddressableIsNotEntered(t *testing.T) {
    collaborator := &atomicallyHeldCollaborator{}

    holder := &atomicPointerHolder{}
    holder.collaborator.Store(collaborator)

    collaboratorIdentity := pointerIdentity{pointer: reflect.ValueOf(collaborator).Pointer(), valueType: reflect.TypeOf(collaborator)}

    holdsCollaborator := func(found []heldPointer) bool {
        for _, held := range found {
            if collaboratorIdentity == held.identity {
                return true
            }
        }

        return false
    }

    if false == holdsCollaborator(walkHeldPointers(holder, nil, nil)) {
        t.Fatalf("expected the collaborator found behind a holder reached through a pointer")
    }

    if true == holdsCollaborator(walkHeldPointers(reflect.ValueOf(holder).Elem().Interface(), nil, nil)) {
        t.Fatalf("expected a holder handed by value not to be entered")
    }
}

type shieldedAtomicCollaborator struct {
    deep *atomicallyHeldCollaborator
}

func (instance *shieldedAtomicCollaborator) CloseWithContext(closeContext context.Context) error {
    return nil
}

type shieldedAtomicHolder struct {
    collaborator atomic.Pointer[shieldedAtomicCollaborator]
}

func (instance *shieldedAtomicHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestHeldPointerIdentities_TheTypeShieldHoldsThroughAnAtomicPointer(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    deep := &atomicallyHeldCollaborator{}
    collaborator := &shieldedAtomicCollaborator{deep: deep}

    serviceContainer.MustRegister("app.deep", func(_ containercontract.Resolver) (*atomicallyHeldCollaborator, error) { return deep, nil }, RegisterOptions{})
    serviceContainer.MustRegister("app.shielded", func(_ containercontract.Resolver) (*shieldedAtomicCollaborator, error) { return collaborator, nil }, RegisterOptions{TeardownReflectionDisabled: true})
    serviceContainer.MustRegister("app.atomic.holder", func(_ containercontract.Resolver) (*shieldedAtomicHolder, error) {
        holder := &shieldedAtomicHolder{}
        holder.collaborator.Store(collaborator)

        return holder, nil
    }, RegisterOptions{})

    verifyDeclaredTeardownDependencies(t, serviceContainer)

    MustFromResolver[*atomicallyHeldCollaborator](t.Context(), serviceContainer, "app.deep")
    MustFromResolver[*shieldedAtomicHolder](t.Context(), serviceContainer, "app.atomic.holder")
    MustFromResolver[*shieldedAtomicCollaborator](t.Context(), serviceContainer, "app.shielded")

    dependenciesOf := make(map[string][]string)
    for _, entry := range serviceContainer.(teardownPlanner).TeardownPlan() {
        dependenciesOf[entry.NodeKey] = entry.Dependencies
    }

    if "[service:app.shielded]" != fmt.Sprint(dependenciesOf["service:app.atomic.holder"]) {
        t.Fatalf("expected the holder to depend on the shielded collaborator alone, got %v", dependenciesOf["service:app.atomic.holder"])
    }
}

func TestHeldPointerIdentities_AnAtomicPointerSwappedWhileWalkedIsNoRace(t *testing.T) {
    for attempt := 0; 20 > attempt; attempt++ {
        serviceContainer := NewContainer(ContainerConfig{})
        stopSwapping := make(chan struct{})
        swapperDone := make(chan struct{})

        serviceContainer.MustRegister("app.swapping", func(_ containercontract.Resolver) (*atomicPointerHolder, error) {
            holder := &atomicPointerHolder{}
            holder.collaborator.Store(&atomicallyHeldCollaborator{label: "first"})

            go func() {
                defer close(swapperDone)

                for {
                    select {
                    case <-stopSwapping:
                        return
                    default:
                        holder.collaborator.Store(&atomicallyHeldCollaborator{label: "next"})
                    }
                }
            }()

            return holder, nil
        }, RegisterOptions{})

        _, walkErr := serviceContainer.Get(t.Context(), "app.swapping")

        close(stopSwapping)
        <-swapperDone

        if nil != walkErr {
            t.Fatalf("unexpected error: %v", walkErr)
        }
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
        identities := walkHeldPointers(&hubHolder{hub: hub}, nil, nil)

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

        identities := walkHeldPointers(&hubHolder{ctx: ctx}, nil, nil)

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

type intermediateStruct struct{ service *closeOrderServiceB }

type outerHolder struct{ middle *intermediateStruct }

func (instance *outerHolder) CloseWithContext(closeContext context.Context) error { return nil }

func TestHeldPointerIdentities_StillFindsAServiceBehindAnIntermediateStruct(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(walkHeldPointers(&outerHolder{middle: &intermediateStruct{service: held}}, nil, nil), heldIdentity) {
        t.Fatalf("expected the service behind an intermediate struct pointer to be found")
    }
}

type chainLink struct {
    next *chainLink
    held *closeOrderServiceB
}

type chainFirstRoot struct {
    chain  *chainLink
    direct *chainLink
}

type directFirstRoot struct {
    direct *chainLink
    chain  *chainLink
}

func chainOfLinks(length int, tail *chainLink) *chainLink {
    head := tail

    for count := 0; count < length; count = count + 1 {
        head = &chainLink{next: head}
    }

    return head
}

func TestHeldPointerIdentities_FindsAServiceAtTheDepthLimitWhicheverFieldComesFirst(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    for length := 3; length <= 7; length = length + 1 {
        tail := &chainLink{held: held}

        if false == holdsIdentity(walkHeldPointers(&chainFirstRoot{chain: chainOfLinks(length, tail), direct: tail}, nil, nil), heldIdentity) {
            t.Fatalf("expected the service to be found with the long path listed first, chain of %d links", length)
        }

        if false == holdsIdentity(walkHeldPointers(&directFirstRoot{chain: chainOfLinks(length, tail), direct: tail}, nil, nil), heldIdentity) {
            t.Fatalf("expected the service to be found with the short path listed first, chain of %d links", length)
        }
    }
}

func TestHeldPointerIdentities_StopsAtTheDepthLimit(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(walkHeldPointers(&chainFirstRoot{chain: chainOfLinks(4, &chainLink{held: held})}, nil, nil), heldIdentity) {
        t.Fatalf("expected a service at the depth limit to be found")
    }

    if true == holdsIdentity(walkHeldPointers(&chainFirstRoot{chain: chainOfLinks(5, &chainLink{held: held})}, nil, nil), heldIdentity) {
        t.Fatalf("expected a service past the depth limit not to be found")
    }
}

type oddDepthTail struct {
    nested struct{ held *closeOrderServiceB }
}

type oddDepthLink struct {
    next *oddDepthLink
    tail *oddDepthTail
}

type oddDepthRoot struct {
    chain *oddDepthLink
}

func (instance *oddDepthRoot) CloseWithContext(closeContext context.Context) error { return nil }

func oddDepthChain(length int, held *closeOrderServiceB) *oddDepthLink {
    tail := &oddDepthTail{}
    tail.nested.held = held

    head := &oddDepthLink{tail: tail}

    for count := 1; count < length; count = count + 1 {
        head = &oddDepthLink{next: head}
    }

    return head
}

/* three links put the held pointer at depth eleven, four at thirteen, one past the limit */
func TestHeldPointerIdentities_DoesNotReadAPointerOnePastTheDepthLimit(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(walkHeldPointers(&oddDepthRoot{chain: oddDepthChain(3, held)}, nil, nil), heldIdentity) {
        t.Fatalf("expected a service at depth eleven to be found")
    }

    if true == holdsIdentity(walkHeldPointers(&oddDepthRoot{chain: oddDepthChain(4, held)}, nil, nil), heldIdentity) {
        t.Fatalf("expected a service one past the depth limit not to be found")
    }
}

type wideHolder struct {
    cells [256][256]struct{ service *closeOrderServiceB }
}

func (instance *wideHolder) CloseWithContext(closeContext context.Context) error { return nil }

type wideHolderWithPeer struct {
    cells [256][256]struct{ service *closeOrderServiceB }
    peer  *closeOrderServiceB
}

func (instance *wideHolderWithPeer) CloseWithContext(closeContext context.Context) error { return nil }

type boundedHolder struct {
    cells [64][256]struct{ service *closeOrderServiceB }
}

func (instance *boundedHolder) CloseWithContext(closeContext context.Context) error { return nil }

func TestHeldPointerIdentities_StopsAtTheNodeBudget(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    last := &wideHolder{}
    last.cells[255][255].service = held

    if true == holdsIdentity(walkHeldPointers(last, nil, nil), heldIdentity) {
        t.Fatalf("expected the walk to stop at its budget before the last cell of a table wider than it")
    }

    beside := &wideHolderWithPeer{peer: held}

    if false == holdsIdentity(walkHeldPointers(beside, nil, nil), heldIdentity) {
        t.Fatalf("expected a peer declared after a table wider than the budget to be found before the table is paid for")
    }

    bounded := &boundedHolder{}
    bounded.cells[63][255].service = held

    if false == holdsIdentity(walkHeldPointers(bounded, nil, nil), heldIdentity) {
        t.Fatalf("expected the last cell of a table within the budget to be reached")
    }
}

func TestContainer_Waves_WalksAValueFiledUnderNameAndTypeOnce(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    registerCapturingPair(t, serviceContainer, recorder)

    if _, getErr := serviceContainer.GetByType(t.Context(), reflect.TypeOf((*capturingHolder)(nil))); nil != getErr {
        t.Fatalf("unexpected get by type error: %v", getErr)
    }

    verifyDeclaredTeardownDependencies(t, serviceContainer)

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

    identities := walkHeldPointers(holder, nil, nil)
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

    /* the length above is the last read of the slice; without this the compiler lets the record die before the collection it is meant to survive */
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

type ringNode struct {
    next     *ringNode
    recorder *closeOrderRecorder
    label    string
}

func (instance *ringNode) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record(instance.label)

    return nil
}

func TestContainer_Close_WavesARingOfHeldCollaboratorsLongerThanTheWalkGainsNoCycle(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
            RegisterOptions{TypeRegistrationDisabled: true},
        ); nil != registerErr {
            t.Fatalf("unexpected register error at %d: %v", index, registerErr)
        }
    }

    for _, node := range nodes {
        if _, getErr := serviceContainer.Get(t.Context(), node.label); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("a ring of held collaborators reported a failure over a teardown that closed every node: %v", closeErr)
    }

    if ringSize != len(closeSequence) {
        t.Fatalf("expected every node of the ring to close, got %v", closeSequence)
    }
}

type resolvingHolder struct {
    recorder *closeOrderRecorder
}

func (instance *resolvingHolder) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("holder")

    return nil
}

type backHolder struct {
    recorder *closeOrderRecorder
    back     *resolvingHolder
}

func (instance *backHolder) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("collaborator")

    return nil
}

func TestContainer_Close_WavesAHeldEdgeAgainstAResolvedEdgeIsNotWritten(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    holder := &resolvingHolder{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.collaborator",
        func(_ containercontract.Resolver) (*backHolder, error) { return &backHolder{recorder: recorder, back: holder}, nil },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*resolvingHolder, error) {
            if _, getErr := FromResolver[*backHolder](resolver.Runtime().Context(), resolver, "app.collaborator"); nil != getErr {
                return nil, getErr
            }

            return holder, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := FromResolver[*resolvingHolder](t.Context(), serviceContainer, "app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("a held edge against a resolved one reported a failure: %v", closeErr)
    }

    if 2 != len(closeSequence) || "holder" != closeSequence[0] || "collaborator" != closeSequence[1] {
        t.Fatalf("expected the resolved order to hold, got %v", closeSequence)
    }
}

type foreignBag struct{ items []*closeOrderServiceB }

type bagHolder struct {
    bag   *foreignBag
    items []*closeOrderServiceB
}

func (instance *bagHolder) CloseWithContext(closeContext context.Context) error { return nil }

func TestHeldPointerIdentities_DoesNotReadASliceEvenInItsOwnMemory(t *testing.T) {
    own := &closeOrderServiceB{}
    ownIdentity, _ := pointerKeyOf(own)

    foreign := &closeOrderServiceB{}
    foreignIdentity, _ := pointerKeyOf(foreign)

    identities := walkHeldPointers(&bagHolder{bag: &foreignBag{items: []*closeOrderServiceB{foreign}}, items: []*closeOrderServiceB{own}}, nil, nil)

    if true == holdsIdentity(identities, ownIdentity) {
        t.Fatalf("expected a service in the holder's own slice to stay unread, since the holder may have been handed that slice")
    }

    if true == holdsIdentity(identities, foreignIdentity) {
        t.Fatalf("expected a slice inside foreign memory to stay unread")
    }
}

type rewritingRegistry struct {
    mutex sync.Mutex
    items []*closeOrderServiceB
}

func (instance *rewritingRegistry) All() []*closeOrderServiceB {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.items
}

func (instance *rewritingRegistry) Rewrite(value *closeOrderServiceB) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.items[0] = value
}

type handedSliceHolder struct {
    items []*closeOrderServiceB
}

func (instance *handedSliceHolder) CloseWithContext(closeContext context.Context) error { return nil }

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
        serviceContainer := NewContainer(ContainerConfig{})

        verifyDeclaredTeardownDependencies(t, serviceContainer)

        if registerErr := serviceContainer.Register(
            "app.holder",
            func(_ containercontract.Resolver) (*handedSliceHolder, error) { return &handedSliceHolder{items: registry.All()}, nil },
            RegisterOptions{},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.Get(t.Context(), "app.holder"); nil != getErr {
            t.Fatalf("unexpected get error: %v", getErr)
        }

        if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
            t.Fatalf("unexpected close error: %v", closeErr)
        }
    }

    close(stop)
    writes.Wait()
}

type mutualPairA struct {
    other    *mutualPairB
    recorder *closeOrderRecorder
}

func (instance *mutualPairA) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("a")

    return nil
}

type mutualPairB struct {
    other    *mutualPairA
    recorder *closeOrderRecorder
}

func (instance *mutualPairB) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("b")

    return nil
}

type pairMemberHolder struct {
    a        *mutualPairA
    recorder *closeOrderRecorder
}

func (instance *pairMemberHolder) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("c")

    return nil
}

func TestContainer_Close_WavesAHeldEdgeBesideAMutualPairIsStillWritten(t *testing.T) {
    var mutex sync.Mutex
    closeSequence := make([]string, 0, 3)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    a := &mutualPairA{recorder: recorder}
    b := &mutualPairB{recorder: recorder, other: a}
    a.other = b
    c := &pairMemberHolder{a: a, recorder: recorder}

    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("pair.a", func(_ containercontract.Resolver) (*mutualPairA, error) { return a, nil }, RegisterOptions{})
    serviceContainer.MustRegister("pair.holder", func(_ containercontract.Resolver) (*pairMemberHolder, error) { return c, nil }, RegisterOptions{})
    serviceContainer.MustRegister(
        "pair.b",
        func(resolver containercontract.Resolver) (*mutualPairB, error) {
            if _, resolveErr := resolver.Get(resolver.Runtime().Context(), "pair.holder"); nil != resolveErr {
                return nil, resolveErr
            }

            return b, nil
        },
        RegisterOptions{},
    )

    MustFromResolver[*mutualPairB](t.Context(), serviceContainer, "pair.b")
    MustFromResolver[*mutualPairA](t.Context(), serviceContainer, "pair.a")

    waveOf := make(map[string]int, 3)
    for _, entry := range serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan() {
        waveOf[entry.NodeKey] = entry.WaveIndex
    }

    if waveOf["service:pair.holder"] >= waveOf["service:pair.a"] {
        t.Fatalf("expected the holder in a wave before the member it holds, got %v", waveOf)
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
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

func TestContainer_Waves_WalksAPublishedServiceAsForeignMemory(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    hub := &foreignHub{backplane: &foreignBackplane{label: "first"}}

    if registerErr := serviceContainer.Register(
        "app.hub",
        func(_ containercontract.Resolver) (*foreignHub, error) { return hub, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.hub"); nil != getErr {
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

    for iteration := 0; iteration < 200; iteration = iteration + 1 {
        walkHeldPointers(hub, nil, nil)
    }

    close(stop)
    writes.Wait()

    for _, held := range serviceContainer.(*container).heldIdentitiesByNodeKey[containerNameNodeKey("app.hub")] {
        if reflect.TypeOf((*foreignBackplane)(nil)) == held.identity.valueType {
            t.Fatalf("expected the backplane behind a built service's interface field to stay unread")
        }
    }
}

type aliasedResolvingService struct {
    recorder *closeOrderRecorder
}

func (instance *aliasedResolvingService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("resolving")

    return nil
}

type aliasedCapturingService struct {
    recorder *closeOrderRecorder
    back     *aliasedResolvingService
}

func (instance *aliasedCapturingService) CloseWithContext(closeContext context.Context) error {
    instance.recorder.record("capturing")

    return nil
}

func TestContainer_Close_WavesAHeldEdgeAgainstAResolvedEdgeThroughATypeAliasIsNotWritten(t *testing.T) {
    for round := 0; round < 50; round = round + 1 {
        serviceContainer := NewContainer(ContainerConfig{})

        verifyDeclaredTeardownDependencies(t, serviceContainer)

        var mutex sync.Mutex
        closeSequence := make([]string, 0, 2)
        recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

        resolving := &aliasedResolvingService{recorder: recorder}

        if registerErr := serviceContainer.Register(
            "app.resolving",
            func(resolver containercontract.Resolver) (*aliasedResolvingService, error) {
                if _, resolveErr := FromResolver[*aliasedCapturingService](resolver.Runtime().Context(), resolver, "app.capturing"); nil != resolveErr {
                    return nil, resolveErr
                }

                return resolving, nil
            },
            RegisterOptions{},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if registerErr := serviceContainer.Register(
            "app.capturing",
            func(_ containercontract.Resolver) (*aliasedCapturingService, error) {
                return &aliasedCapturingService{recorder: recorder, back: resolving}, nil
            },
            RegisterOptions{TypeRegistrationDisabled: true},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.GetByType(t.Context(), reflect.TypeOf((*aliasedResolvingService)(nil))); nil != getErr {
            t.Fatalf("unexpected resolution by type error: %v", getErr)
        }

        if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
            t.Fatalf("round %d: a held edge against a resolved edge, one alias away, was written and reported as a cycle: %v", round, closeErr)
        }

        if 2 != len(closeSequence) {
            t.Fatalf("round %d: expected both services to close, got %v", round, closeSequence)
        }
    }
}

func TestContainer_Close_ReleasesTheHeldIdentityRecords(t *testing.T) {
    finalized := make(chan struct{}, 1)

    serviceContainer := NewContainer(ContainerConfig{})

    defer runtime.KeepAlive(serviceContainer)

    verifyDeclaredTeardownDependencies(t, serviceContainer)

    holder := &capturingHolder{recorder: &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}, held: &closeOrderServiceB{}}
    runtime.SetFinalizer(holder.held, func(*closeOrderServiceB) { finalized <- struct{}{} })

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return holder, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    holder.held = nil

    for cycle := 0; cycle < 3; cycle = cycle + 1 {
        runtime.GC()
    }

    select {
    case <-finalized:
        t.Fatalf("expected the record to keep the dropped collaborator alive until the teardown")
    case <-time.After(50 * time.Millisecond):
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    for cycle := 0; cycle < 5; cycle = cycle + 1 {
        runtime.GC()

        select {
        case <-finalized:
            return
        case <-time.After(20 * time.Millisecond):
        }
    }

    t.Fatalf("expected the collaborator to be collectable once the container had closed")
}

func TestContainer_TeardownPlan_DoesNotWriteAnInferredEdgeIntoTheGraph(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return &capturingHolder{recorder: recorder, held: held}, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    buildEveryRegisteredService(t, serviceContainer, "app.held", "app.holder")

    serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    if _, getErr := lateResolver.Get(t.Context(), "app.holder"); nil != getErr {
        t.Fatalf("unexpected late resolution error: %v", getErr)
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("an inference the view had written into the graph closed a ring with a later resolution: %v", closeErr)
    }

    if 2 != len(closeSequence) || "b" != closeSequence[0] || "holder" != closeSequence[1] {
        t.Fatalf("expected the resolved order to hold over the inference, got %v", closeSequence)
    }
}

func TestContainer_Waves_AnUnbuiltDeclaredDependencyIsNotAWayBack(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    held := &closeOrderServiceB{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.held",
        func(_ containercontract.Resolver) (*closeOrderServiceB, error) { return held, nil },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"app.unbuilt"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.unbuilt",
        func(_ containercontract.Resolver) (*closeOrderServiceA, error) { return &closeOrderServiceA{recorder: recorder}, nil },
        RegisterOptions{TypeRegistrationDisabled: true, TeardownDependencyNames: []string{"app.holder"}},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return &capturingHolder{recorder: recorder, held: held}, nil },
        RegisterOptions{TypeRegistrationDisabled: true},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    verifyDeclaredTeardownDependencies(t, serviceContainer)

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

type fatHub struct {
    cells [64][256]struct{ left, right *closeOrderServiceB }
}

type hubLink struct {
    next *hubLink
    hub  *fatHub
}

func newHubChain(hub *fatHub, length int) *hubLink {
    var head *hubLink

    for index := 0; index < length; index = index + 1 {
        head = &hubLink{next: head, hub: hub}
    }

    return head
}

type collaboratorLink struct {
    next *collaboratorLink
    held *closeOrderServiceB
}

func newCollaboratorChain(held *closeOrderServiceB, length int) *collaboratorLink {
    head := &collaboratorLink{held: held}

    for index := 1; index < length; index = index + 1 {
        head = &collaboratorLink{next: head}
    }

    return head
}

type chainFirstHolder struct {
    chain        *hubLink
    hub          *fatHub
    collaborator *collaboratorLink
}

func (instance *chainFirstHolder) CloseWithContext(closeContext context.Context) error { return nil }

type collaboratorFirstHolder struct {
    collaborator *collaboratorLink
    hub          *fatHub
    chain        *hubLink
}

func (instance *collaboratorFirstHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestHeldPointerIdentities_DoesNotWalkAWholeSubtreeAgainFromAShallowerPath(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)
    hub := &fatHub{}

    if false == holdsIdentity(walkHeldPointers(&collaboratorFirstHolder{collaborator: newCollaboratorChain(held, 4), hub: hub, chain: newHubChain(hub, 2)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the collaborator declared first to be found")
    }

    if false == holdsIdentity(walkHeldPointers(&chainFirstHolder{chain: newHubChain(hub, 2), hub: hub, collaborator: newCollaboratorChain(held, 4)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the collaborator declared after a chain of links to one large object to be found as well")
    }
}

type tableFirstCodec struct {
    table [256][256]byte
    peer  *collaboratorLink
}

func (instance *tableFirstCodec) CloseWithContext(closeContext context.Context) error { return nil }

type peerFirstCodec struct {
    peer  *collaboratorLink
    table [256][256]byte
}

func (instance *peerFirstCodec) CloseWithContext(closeContext context.Context) error { return nil }

/* sixty-four kibibytes at one budget unit per byte is the whole budget */
func TestHeldPointerIdentities_CountsAnArrayOfScalarsAsOneNode(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(walkHeldPointers(&peerFirstCodec{peer: newCollaboratorChain(held, 1)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the peer declared before the table to be found")
    }

    if false == holdsIdentity(walkHeldPointers(&tableFirstCodec{peer: newCollaboratorChain(held, 1)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the peer declared after the table to be found as well")
    }
}

func structOfScalarsAsWideAsTheBudget() reflect.Type {
    integerFields := make([]reflect.StructField, 0, TeardownWalkElementsRule().Default)
    for index := 0; index < TeardownWalkElementsRule().Default; index = index + 1 {
        integerFields = append(integerFields, reflect.StructField{Name: reflectStructFieldName("Scalar", index), Type: reflect.TypeOf(0)})
    }

    inner := reflect.StructOf(integerFields)

    innerFields := make([]reflect.StructField, 0, TeardownWalkElementsRule().Default)
    for index := 0; index < TeardownWalkElementsRule().Default; index = index + 1 {
        innerFields = append(innerFields, reflect.StructField{Name: reflectStructFieldName("Inner", index), Type: inner})
    }

    return reflect.StructOf(innerFields)
}

func reflectStructFieldName(prefix string, index int) string {
    return prefix + string(rune('A'+index/26)) + string(rune('A'+index%26))
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

    if false == holdsIdentity(walkHeldPointers(root.Interface(), nil, nil), heldIdentity) {
        t.Fatalf("expected the peer one link below the struct of scalars to be found: the struct is one node, not one per field")
    }
}

type cellTable struct {
    cells [16][256][256]struct{ pointer *closeOrderServiceB }
    peer  *collaboratorLink
}

func TestHeldPointerIdentities_ChargesTheBudgetWhenAnItemIsQueued(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)
    table := &cellTable{peer: newCollaboratorChain(held, 1)}

    runtime.GC()

    var before runtime.MemStats
    runtime.ReadMemStats(&before)

    found := holdsIdentity(walkHeldPointers(table, nil, nil), heldIdentity)

    var after runtime.MemStats
    runtime.ReadMemStats(&after)

    runtime.KeepAlive(table)

    /* a million cells: about two hundred megabytes charged at the taking, under four charged at the queue */
    allocated := after.TotalAlloc - before.TotalAlloc
    if 16<<20 < allocated {
        t.Fatalf("expected the walk over a million pointer-holding cells to queue no more than the budget, it allocated %d bytes", allocated)
    }

    if false == found {
        t.Fatalf("expected the peer one link below the table to be found: it is queued at the depth of the table's rows, before any cell is charged")
    }
}

type cycleMemberLink struct {
    next *cycleMemberLink
    head *cycleMemberHead
}

type cycleMemberHead struct {
    leaf *cycleMemberLeaf
    tail *cycleMemberTail
}

type cycleMemberTail struct {
    head *cycleMemberHead
}

type cycleMemberLeaf struct {
    next         *cycleMemberLeaf
    collaborator *closeOrderServiceB
}

type chainThenTailHolder struct {
    chain *cycleMemberLink
    tail  *cycleMemberTail
}

func (instance *chainThenTailHolder) CloseWithContext(closeContext context.Context) error { return nil }

type tailThenChainHolder struct {
    tail  *cycleMemberTail
    chain *cycleMemberLink
}

func (instance *tailThenChainHolder) CloseWithContext(closeContext context.Context) error { return nil }

func newCycleMemberShape(held *closeOrderServiceB) (*cycleMemberLink, *cycleMemberTail) {
    head := &cycleMemberHead{leaf: &cycleMemberLeaf{next: &cycleMemberLeaf{next: &cycleMemberLeaf{collaborator: held}}}}
    tail := &cycleMemberTail{head: head}
    head.tail = tail

    return &cycleMemberLink{next: &cycleMemberLink{head: head}}, tail
}

func TestHeldPointerIdentities_FindsAHeldServiceBehindARingFromTheShortPathWhicheverFieldComesFirst(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    chain, tail := newCycleMemberShape(held)

    if false == holdsIdentity(walkHeldPointers(&tailThenChainHolder{tail: tail, chain: chain}, nil, nil), heldIdentity) {
        t.Fatalf("expected the collaborator found through the short path declared first")
    }

    chain, tail = newCycleMemberShape(held)

    if false == holdsIdentity(walkHeldPointers(&chainThenTailHolder{chain: chain, tail: tail}, nil, nil), heldIdentity) {
        t.Fatalf("expected the collaborator found through the short path declared after the chain that cut it")
    }
}

type cyclicHub struct {
    cells [64][256]struct{ left, right *closeOrderServiceB }
    back  *cyclicHubLink
}

type cyclicHubLink struct {
    next *cyclicHubLink
    hub  *cyclicHub
}

type chainFirstCyclicHubHolder struct {
    chain        *cyclicHubLink
    hub          *cyclicHub
    collaborator *closeOrderServiceB
}

func (instance *chainFirstCyclicHubHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestHeldPointerIdentities_AHubOnARingIsPaidForOnceAndTheCollaboratorAfterItIsFound(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    hub := &cyclicHub{}
    tail := &cyclicHubLink{hub: hub}
    head := &cyclicHubLink{next: tail, hub: hub}
    hub.back = head

    if false == holdsIdentity(walkHeldPointers(&chainFirstCyclicHubHolder{chain: head, hub: hub, collaborator: held}, nil, nil), heldIdentity) {
        t.Fatalf("expected the collaborator declared after a cyclic hub walked whole once to be found")
    }
}

type crossEdgeNode struct {
    next  *crossEdgeNode
    cross *crossEdgeNode
    back  *crossEdgeNode
    held  *closeOrderServiceB
}

type crossEdgeHolder struct {
    chain *crossEdgeNode
    short *crossEdgeNode
}

func (instance *crossEdgeHolder) CloseWithContext(closeContext context.Context) error { return nil }

func newCrossEdgeShape(held *closeOrderServiceB) (chain *crossEdgeNode, holderOfMember *crossEdgeNode) {
    beyondTheLimit := &crossEdgeNode{held: held}
    underTheRoot := &crossEdgeNode{next: beyondTheLimit}
    root := &crossEdgeNode{next: underTheRoot}
    member := &crossEdgeNode{back: root}
    holderOfMember = &crossEdgeNode{cross: member}
    root.cross = member
    root.back = holderOfMember

    chain = root
    for link := 0; link < 3; link = link + 1 {
        chain = &crossEdgeNode{next: chain}
    }

    return chain, holderOfMember
}

func TestHeldPointerIdentities_FindsAHeldServiceBehindARingHeldThroughAForwardEdgeFromTheShortPath(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    chain, holderOfMember := newCrossEdgeShape(held)

    if false == holdsIdentity(walkHeldPointers(&crossEdgeHolder{chain: chain, short: holderOfMember}, nil, nil), heldIdentity) {
        t.Fatalf("expected the collaborator found through the short path into the holder of a cycle member")
    }
}

type anyTableFirstCodec struct {
    table [256][256]any
    peer  *collaboratorLink
}

func (instance *anyTableFirstCodec) CloseWithContext(closeContext context.Context) error { return nil }

type sliceTableFirstCodec struct {
    table [256][256][]byte
    peer  *collaboratorLink
}

func (instance *sliceTableFirstCodec) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestHeldPointerIdentities_CountsAnArrayOfWhatItDoesNotReadAsOneNode(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(walkHeldPointers(&anyTableFirstCodec{peer: newCollaboratorChain(held, 1)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the peer declared after a table of interfaces to be found")
    }

    if false == holdsIdentity(walkHeldPointers(&sliceTableFirstCodec{peer: newCollaboratorChain(held, 1)}, nil, nil), heldIdentity) {
        t.Fatalf("expected the peer declared after a table of slices to be found")
    }
}

type selfReferringHolder struct {
    self         *selfReferringHolder
    collaborator *closeOrderServiceB
}

func (instance *selfReferringHolder) CloseWithContext(closeContext context.Context) error { return nil }

func TestHeldPointerIdentities_ReadsAValuePointingAtItselfOnce(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    holder := &selfReferringHolder{collaborator: held}
    holder.self = holder

    identities := walkHeldPointers(holder, nil, nil)

    if false == holdsIdentity(identities, heldIdentity) {
        t.Fatalf("expected the collaborator of a self-referring holder to be found")
    }

    if 2 != len(identities) {
        t.Fatalf("expected the holder and its collaborator recorded once each, got %d", len(identities))
    }
}

type listenerBus struct {
    listeners []*busListener
}

func (instance *listenerBus) CloseWithContext(closeContext context.Context) error { return nil }

type busListener struct {
    back     *listenerBus
    inFlight *atomic.Int32
    peak     *atomic.Int32
}

func (instance *busListener) CloseWithContext(closeContext context.Context) error {
    now := instance.inFlight.Add(1)

    for {
        peak := instance.peak.Load()
        if now <= peak || true == instance.peak.CompareAndSwap(peak, now) {
            break
        }
    }

    deadline := time.Now().Add(2 * time.Second)
    for 2 > instance.peak.Load() && true == time.Now().Before(deadline) {
        time.Sleep(time.Millisecond)
    }

    instance.inFlight.Add(-1)

    return nil
}

func TestContainer_Close_WavesTwoListenersHoldingTheirBusBackStillCloseTogether(t *testing.T) {
    var inFlight atomic.Int32
    var peak atomic.Int32

    serviceContainer := NewContainer(ContainerConfig{})

    /* the bus is allocated before the listeners, so each listener holds it when it is filed and walked */
    bus := &listenerBus{}

    for _, name := range []string{"app.p", "app.q"} {
        serviceContainer.MustRegister(
            name,
            func(_ containercontract.Resolver) (*busListener, error) {
                return &busListener{back: bus, inFlight: &inFlight, peak: &peak}, nil
            },
            RegisterOptions{TypeRegistrationDisabled: true},
        )
    }

    serviceContainer.MustRegister(
        "app.bus",
        func(resolver containercontract.Resolver) (*listenerBus, error) {
            for _, name := range []string{"app.p", "app.q"} {
                listener, resolveErr := FromResolver[*busListener](resolver.Runtime().Context(), resolver, name)
                if nil != resolveErr {
                    return nil, resolveErr
                }

                bus.listeners = append(bus.listeners, listener)
            }

            return bus, nil
        },
        RegisterOptions{},
    )

    MustFromResolver[*listenerBus](t.Context(), serviceContainer, "app.bus")

    listenerRecord, recorded := heldRecordOf(serviceContainer, containerNameNodeKey("app.p"))
    busIdentity, _ := pointerKeyOf(bus)
    if false == recorded || false == holdsIdentity(listenerRecord, busIdentity) {
        t.Fatalf("expected the listener's walk to record the bus it holds, got %v", listenerRecord)
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 2 != peak.Load() {
        t.Fatalf("expected the two listeners, unrelated to each other, closed together in the wave past their bus, got a peak of %d", peak.Load())
    }
}

func TestHeldPointerIdentities_ReadsAPreBuiltValueAProviderHandsBackAsPublishedMemory(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

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
            RegisterOptions{TypeRegistrationDisabled: true},
        ); nil != registerErr {
            t.Fatalf("unexpected register error: %v", registerErr)
        }

        if _, getErr := serviceContainer.Get(t.Context(), fmt.Sprintf("app.hub.%d", round)); nil != getErr {
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

    serviceContainer := NewContainer(ContainerConfig{})

    serviceContainer.MustRegister("pair.a", func(_ containercontract.Resolver) (*mutualPairA, error) { return a, nil }, RegisterOptions{})
    serviceContainer.MustRegister("pair.b", func(_ containercontract.Resolver) (*mutualPairB, error) { return b, nil }, RegisterOptions{})
    serviceContainer.MustRegister("pair.alone", func(_ containercontract.Resolver) (*closeOrderServiceC, error) { return &closeOrderServiceC{recorder: recorder}, nil }, RegisterOptions{})

    MustFromResolver[*mutualPairA](t.Context(), serviceContainer, "pair.a")
    MustFromResolver[*mutualPairB](t.Context(), serviceContainer, "pair.b")
    MustFromResolver[*closeOrderServiceC](t.Context(), serviceContainer, "pair.alone")

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

/* zeroSizeHeldService is a service of zero size: every such allocation shares one address, so a pointer to one cannot say which service it names. */
type zeroSizeHeldService struct{}

func (instance *zeroSizeHeldService) CloseWithContext(closeContext context.Context) error {
    return nil
}

type zeroSizeHolder struct {
    held *zeroSizeHeldService
}

func (instance *zeroSizeHolder) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestContainer_Waves_AHeldZeroSizePointerNamesNoServiceAndGainsNoEdge(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    MustRegister[*zeroSizeHeldService](
        serviceContainer,
        "zero.size.service",
        func(resolver containercontract.Resolver) (*zeroSizeHeldService, error) {
            return &zeroSizeHeldService{}, nil
        },
        RegisterOptions{},
    )

    MustRegister[*zeroSizeHolder](
        serviceContainer,
        "zero.size.holder",
        func(resolver containercontract.Resolver) (*zeroSizeHolder, error) {
            return &zeroSizeHolder{held: &zeroSizeHeldService{}}, nil
        },
        RegisterOptions{},
    )

    _ = MustFromResolver[*zeroSizeHeldService](t.Context(), serviceContainer, "zero.size.service")
    _ = MustFromResolver[*zeroSizeHolder](t.Context(), serviceContainer, "zero.size.holder")

    planned, carriesPlan := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    })
    if false == carriesPlan {
        t.Fatalf("expected the container to carry the teardown plan door")
    }

    for _, entry := range planned.TeardownPlan() {
        if "service:zero.size.holder" != entry.NodeKey {
            continue
        }

        if 0 != len(entry.Dependencies) {
            t.Fatalf("expected the zero-size pointer the holder carries to name no service, got dependencies %v", entry.Dependencies)
        }

        return
    }

    t.Fatalf("expected the holder in the teardown plan")
}

type selfPointingNode struct {
    next *selfPointingNode
}

func TestTypeCanHoldIdentity_AnswersTrueForAStructPointingAtItselfWithoutRecursing(t *testing.T) {
    if false == typeCanHoldIdentity(reflect.TypeOf(selfPointingNode{})) {
        t.Fatalf("expected a struct holding a pointer to itself to be able to hold an identity")
    }

    if false == typeCanHoldIdentity(reflect.TypeOf([2]selfPointingNode{})) {
        t.Fatalf("expected an array of such structs to be able to hold an identity")
    }

    if true == typeCanHoldIdentity(reflect.TypeOf([3]struct{ count int }{})) {
        t.Fatalf("expected an array of scalar structs to hold no identity")
    }
}

func TestHeldPointerIdentities_MatchesTheBoundedReachabilityOfARandomGraph(t *testing.T) {
    const nodeCount = 18

    random := rand.New(rand.NewSource(20260911))

    for sample := 0; sample < 500; sample++ {
        nodes := make([]*reflectionGraphNode, nodeCount)
        for index := range nodes {
            nodes[index] = &reflectionGraphNode{value: index}
        }

        edges := make([][2]int, nodeCount)
        for index, node := range nodes {
            edges[index] = [2]int{random.Intn(nodeCount+1) - 1, random.Intn(nodeCount+1) - 1}
            if 0 <= edges[index][0] {
                node.left = nodes[edges[index][0]]
            }
            if 0 <= edges[index][1] {
                node.right = nodes[edges[index][1]]
            }
        }

        distances := make([]int, nodeCount)
        for index := range distances {
            distances[index] = -1
        }
        distances[0] = 0
        pending := []int{0}
        for head := 0; head < len(pending); head++ {
            current := pending[head]
            for _, next := range edges[current] {
                if 0 <= next && 0 > distances[next] {
                    distances[next] = distances[current] + 1
                    pending = append(pending, next)
                }
            }
        }

        held := walkHeldPointers(nodes[0], nil, nil)
        for index, node := range nodes {
            identity, _ := pointerKeyOf(node)
            /* each link is a pointer and a struct, two levels of the walk */
            wanted := 0 <= distances[index] && 2*distances[index] <= TeardownWalkDepthRule().Default
            if wanted != holdsIdentity(held, identity) {
                t.Fatalf("sample %d node %d at distance %d: expected held=%v over the edges %v", sample, index, distances[index], wanted, edges)
            }
        }
    }
}

type reflectionStorageInner struct{}

type reflectionStorage struct {
    inner *reflectionStorageInner
    pad   int
}

type reflectionStorageHolder struct {
    storage *reflectionStorage
}

func registerStorageFiledBeforeItsCapturingHolder(t *testing.T, serviceContainer containercontract.Container, skipReflection bool) *reflectionStorage {
    t.Helper()

    captured := &reflectionStorage{inner: &reflectionStorageInner{}}

    storageOptions := RegisterOptions{TeardownReflectionDisabled: skipReflection}

    if registerErr := serviceContainer.Register(
        "app.storage",
        func(resolver containercontract.Resolver) (*reflectionStorage, error) {
            return captured, nil
        },
        storageOptions,
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*reflectionStorageHolder, error) {
            return &reflectionStorageHolder{storage: captured}, nil
        },
        RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := FromResolver[*reflectionStorage](t.Context(), serviceContainer, "app.storage"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*reflectionStorageHolder](t.Context(), serviceContainer, "app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    return captured
}

func heldRecordOf(serviceContainer containercontract.Container, nodeKey string) ([]heldPointer, bool) {
    concrete := serviceContainer.(*container)

    concrete.mutex.RLock()
    defer concrete.mutex.RUnlock()

    record, recorded := concrete.heldIdentitiesByNodeKey[nodeKey]

    return record, recorded
}

func TestContainer_Waves_AServiceRegisteredWithTeardownReflectionDisabledIsHeldButNeverEntered(t *testing.T) {
    for _, skipReflection := range []bool{false, true} {
        serviceContainer := NewContainer(ContainerConfig{})
        verifyDeclaredTeardownDependencies(t, serviceContainer)

        captured := registerStorageFiledBeforeItsCapturingHolder(t, serviceContainer, skipReflection)

        storageIdentity, _ := pointerKeyOf(captured)
        innerIdentity, _ := pointerKeyOf(captured.inner)

        holderRecord, holderRecorded := heldRecordOf(serviceContainer, containerNameNodeKey("app.holder"))
        if false == holderRecorded || false == holdsIdentity(holderRecord, storageIdentity) {
            t.Fatalf("skip=%v: expected the holder's walk to record the storage it holds, got %v", skipReflection, holderRecord)
        }

        if skipReflection == holdsIdentity(holderRecord, innerIdentity) {
            t.Fatalf("skip=%v: expected the holder's walk to enter the storage only when it was registered with reflection, got %v", skipReflection, holderRecord)
        }

        if _, storageRecorded := heldRecordOf(serviceContainer, containerNameNodeKey("app.storage")); skipReflection == storageRecorded {
            t.Fatalf("skip=%v: expected the storage to be walked only when it was registered with reflection", skipReflection)
        }
    }
}

func TestContainer_Waves_AnOverrideIsNotWalkedUnderTheTypeOfANameRegisteredWithTeardownReflectionDisabled(t *testing.T) {
    for _, skipReflection := range []bool{false, true} {
        serviceContainer := NewContainer(ContainerConfig{})
        verifyDeclaredTeardownDependencies(t, serviceContainer)

        _ = registerStorageFiledBeforeItsCapturingHolder(t, serviceContainer, skipReflection)

        serviceContainer.MustOverrideInstance("app.storage", &reflectionStorage{inner: &reflectionStorageInner{}}, OverrideOptions{})

        typeNodeKey := containerTypeNodeKey(reflect.TypeOf((*reflectionStorage)(nil)))
        if _, typeRecorded := heldRecordOf(serviceContainer, typeNodeKey); skipReflection == typeRecorded {
            t.Fatalf("skip=%v: expected the type node to be walked only when the registration kept reflection", skipReflection)
        }
    }
}

func TestContainer_Waves_AnOverrideOfANameRegisteredWithTeardownReflectionDisabledIsNotWalked(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    verifyDeclaredTeardownDependencies(t, serviceContainer)

    _ = registerStorageFiledBeforeItsCapturingHolder(t, serviceContainer, true)

    serviceContainer.MustOverrideInstance("app.storage", &reflectionStorage{inner: &reflectionStorageInner{}}, OverrideOptions{})

    if _, storageRecorded := heldRecordOf(serviceContainer, containerNameNodeKey("app.storage")); true == storageRecorded {
        t.Fatalf("expected the override of a name registered without reflection not to be walked")
    }
}

func TestContainer_Waves_AServiceRegisteredWithTeardownReflectionDisabledIsStillOneWavePastItsHolder(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    verifyDeclaredTeardownDependencies(t, serviceContainer)

    _ = registerStorageFiledBeforeItsCapturingHolder(t, serviceContainer, true)

    waveByNode := make(map[string]int)
    for _, entry := range serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan() {
        waveByNode[entry.NodeKey] = entry.WaveIndex
    }

    holderWave, holderPlanned := waveByNode["service:app.holder"]
    storageWave, storagePlanned := waveByNode["service:app.storage"]
    if false == holderPlanned || false == storagePlanned || holderWave >= storageWave {
        t.Fatalf("expected the storage at least one wave past its holder, got %v", waveByNode)
    }
}

type reflectionBusyLink struct {
    next *reflectionBusyLink
}

type reflectionBusyService struct {
    mutex sync.Mutex
    head  *reflectionBusyLink
    stop  chan struct{}
    done  chan struct{}
}

func newReflectionBusyService() *reflectionBusyService {
    service := &reflectionBusyService{stop: make(chan struct{}), done: make(chan struct{})}
    started := make(chan struct{})

    go func() {
        defer close(service.done)
        close(started)

        for {
            select {
            case <-service.stop:
                return
            default:
            }

            service.mutex.Lock()
            service.head = &reflectionBusyLink{next: service.head}
            service.head.next = nil
            service.mutex.Unlock()

            runtime.Gosched()
        }
    }()

    <-started

    return service
}

func (instance *reflectionBusyService) CloseWithContext(closeContext context.Context) error {
    close(instance.stop)
    <-instance.done

    return nil
}

type reflectionBusyHolder struct {
    busy *reflectionBusyService
}

/* a data race is visible only under -race, which the gate's race lane runs */
func TestContainer_Waves_ABusyServiceRegisteredWithTeardownReflectionDisabledIsNeverRead(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})
    verifyDeclaredTeardownDependencies(t, serviceContainer)

    MustRegister[*reflectionBusyService](serviceContainer, "app.busy", func(resolver containercontract.Resolver) (*reflectionBusyService, error) {
        return newReflectionBusyService(), nil
    }, RegisterOptions{TeardownReflectionDisabled: true})

    MustRegister[*reflectionBusyHolder](serviceContainer, "app.busy.holder", func(resolver containercontract.Resolver) (*reflectionBusyHolder, error) {
        busy, busyErr := FromResolver[*reflectionBusyService](resolver.Runtime().Context(), resolver, "app.busy")
        if nil != busyErr {
            return nil, busyErr
        }

        return &reflectionBusyHolder{busy: busy}, nil
    }, RegisterOptions{})

    MustFromResolver[*reflectionBusyHolder](t.Context(), serviceContainer, "app.busy.holder")

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }
}

func TestContainer_Waves_AHolderResolvedFirstNeverEntersAServiceRegisteredWithTeardownReflectionDisabled(t *testing.T) {
    serviceContainer := NewContainer(ContainerConfig{})

    captured := &reflectionStorage{inner: &reflectionStorageInner{}}

    serviceContainer.MustRegister("app.storage", func(_ containercontract.Resolver) (*reflectionStorage, error) { return captured, nil }, RegisterOptions{TeardownReflectionDisabled: true})
    serviceContainer.MustRegister("app.holder", func(_ containercontract.Resolver) (*reflectionStorageHolder, error) { return &reflectionStorageHolder{storage: captured}, nil }, RegisterOptions{})

    MustFromResolver[*reflectionStorageHolder](t.Context(), serviceContainer, "app.holder")

    storageIdentity, _ := pointerKeyOf(captured)
    innerIdentity, _ := pointerKeyOf(captured.inner)

    holderRecord, _ := heldRecordOf(serviceContainer, containerNameNodeKey("app.holder"))
    if false == holdsIdentity(holderRecord, storageIdentity) || true == holdsIdentity(holderRecord, innerIdentity) {
        t.Fatalf("expected the holder to hold the storage without the walk entering it, got %v", holderRecord)
    }
}

func TestHeldPointerIdentities_ARaisedDepthReachesPastTheDefault(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    raised := defaultTeardownWalk()
    raised.depth = 2 * raised.depth

    found, budgetEnded := heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(5, &chainLink{held: held})}, nil, nil, raised)
    if false == holdsIdentity(found, heldIdentity) || true == budgetEnded {
        t.Fatalf("expected a raised depth to reach the service the default does not, found %v, budget ended %v", holdsIdentity(found, heldIdentity), budgetEnded)
    }

    lowered := defaultTeardownWalk()
    lowered.depth = 1

    if found, _ := heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(1, &chainLink{held: held})}, nil, nil, lowered); true == holdsIdentity(found, heldIdentity) {
        t.Fatalf("expected a depth of 1 to stop before a service two links down")
    }
}

func TestHeldPointerIdentities_TheElementBoundCutsTheFieldsEntered(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    limits := defaultTeardownWalk()
    limits.elements = 1

    if found, _ := heldPointerIdentities(&chainFirstRoot{direct: &chainLink{held: held}}, nil, nil, limits); true == holdsIdentity(found, heldIdentity) {
        t.Fatalf("expected an element bound of 1 to leave the second field unentered")
    }

    limits.elements = 2

    if found, _ := heldPointerIdentities(&chainFirstRoot{direct: &chainLink{held: held}}, nil, nil, limits); false == holdsIdentity(found, heldIdentity) {
        t.Fatalf("expected an element bound of 2 to enter the second field")
    }
}

func TestHeldPointerIdentities_ReportsTheNodeBudgetEndingTheWalk(t *testing.T) {
    held := &closeOrderServiceB{}

    if _, budgetEnded := heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(3, &chainLink{held: held})}, nil, nil, defaultTeardownWalk()); true == budgetEnded {
        t.Fatalf("expected the default budget to walk a short chain whole")
    }

    limits := defaultTeardownWalk()
    limits.nodes = 2

    found, budgetEnded := heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(3, &chainLink{held: held})}, nil, nil, limits)
    if false == budgetEnded {
        t.Fatalf("expected a budget of 2 items to be reported as ending the walk")
    }

    heldIdentity, _ := pointerKeyOf(held)
    if true == holdsIdentity(found, heldIdentity) {
        t.Fatalf("expected the service past the budget not to be found")
    }
}

type arrayHolder struct {
    items [3]*closeOrderServiceB
}

func TestHeldPointerIdentities_TheElementBoundCutsTheArrayElementsEntered(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    limits := defaultTeardownWalk()
    limits.elements = 2

    if found, _ := heldPointerIdentities(&arrayHolder{items: [3]*closeOrderServiceB{nil, nil, held}}, nil, nil, limits); true == holdsIdentity(found, heldIdentity) {
        t.Fatalf("expected an element bound of 2 to leave the third element unentered")
    }

    limits.elements = 3

    if found, _ := heldPointerIdentities(&arrayHolder{items: [3]*closeOrderServiceB{nil, nil, held}}, nil, nil, limits); false == holdsIdentity(found, heldIdentity) {
        t.Fatalf("expected an element bound of 3 to enter the third element")
    }
}
