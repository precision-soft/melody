package container

import (
    "context"
    "fmt"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* capturingHolder is the shape the reflection walk exists for: a service that HOLDS a collaborator it never resolved, because its provider closed over a value built before it. Nothing about it writes an edge. */
type capturingHolder struct {
    recorder *closeOrderRecorder
    held     *closeOrderServiceB
}

func (instance *capturingHolder) Close() error {
    instance.recorder.record("holder")

    return nil
}

func registerCapturingPair(t *testing.T, serviceContainer containercontract.Container, recorder *closeOrderRecorder) {
    t.Helper()

    /* built before the container, the way a composition root builds what it then publishes: the provider below has nothing to resolve and hands it back */
    captured := &closeOrderServiceB{recorder: recorder}

    if registerErr := serviceContainer.Register(
        "app.storage",
        func(resolver containercontract.Resolver) (*closeOrderServiceB, error) {
            return captured, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(resolver containercontract.Resolver) (*capturingHolder, error) {
            return &capturingHolder{recorder: recorder, held: captured}, nil
        },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    /* the holder is built FIRST, so the creation stamp alone closes the storage before it — which is the wrong order, and the accident the walk has to correct */
    if _, getErr := FromResolver[*capturingHolder](serviceContainer, "app.holder"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    if _, getErr := FromResolver[*closeOrderServiceB](serviceContainer, "app.storage"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }
}

/* the holder captured its collaborator instead of resolving it, so no edge was ever written and the creation order alone closes the collaborator FIRST — under the holder that is still using it. Armed, the walk sees what the holder holds and the graph gains the edge the provider did not write. */
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

/* the sequence above is the property, but on its own it cannot pin the edge: with no edge the two land in ONE wave and are closed at the same moment, so the order recorded is whatever the scheduler chose and the assertion passes about half the time. The wave is what nothing can decide by luck — a dependency is one past its last dependent, or it is beside it. */
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

/* the sibling that makes the test above an observation rather than a coincidence: the same pair, not armed, closes in the order the creation stamp gives — the collaborator first, under the service still holding it. It is the state this repository has shipped, and it is what the walk changes. */
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

/* a map is neither counted nor entered, because iterating one another goroutine writes is a fatal error no recover catches. The cost is a real edge the walk cannot see, and it is written down here so the limit is a decision rather than an oversight. */
func TestHeldPointerIdentities_DoesNotEnterAMap(t *testing.T) {
    held := &closeOrderServiceB{}

    throughField := heldPointerIdentities(&capturingHolder{held: held}, false)
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

    throughMap := heldPointerIdentities(map[string]*closeOrderServiceB{"held": held}, false)
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

/* mutualParentService and mutualChildService hold each other, which is what a parent and a child with a back-pointer are and what the reflection walk has to answer about: each pointer is evidence that the other must outlive it, and the two together are evidence of nothing. */
type mutualParentService struct {
    recorder *closeOrderRecorder
    child    *mutualChildService
}

func (instance *mutualParentService) Close() error {
    instance.recorder.record("parent")

    return nil
}

type mutualChildService struct {
    recorder *closeOrderRecorder
    parent   *mutualParentService
}

func (instance *mutualChildService) Close() error {
    instance.recorder.record("child")

    return nil
}

/* two services that hold EACH OTHER gain no edge at all, so an armed teardown closes them in the order it closed them before and reports nothing. Written both ways the pair is a ring, and a ring fails a teardown in which every service closed — measured on this very fixture, unarmed nil against armed "dependency cycle detected" over two closes that both answered nil. Dropping both is the rule the walk already states about itself: an edge it does not reach is an edge the graph does not gain, and the pair keeps the position the sequential teardown gave it. The sibling above, where the holding goes ONE way, is what keeps this from being a filter that swallows the walk's whole purpose. */
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

/* foreignHub is the shape of a PUBLISHED collaborator: an interface field its owner rewrites under its own mutex, while the walk of a service holding the hub reads without one. */
type foreignHub struct {
    mutex     sync.Mutex
    backplane fmt.Stringer
}

func (instance *foreignHub) swap(backplane fmt.Stringer) {
    instance.mutex.Lock()
    instance.backplane = backplane
    instance.mutex.Unlock()
}

func (instance *foreignHub) Close() error { return nil }

type foreignBackplane struct{ label string }

func (instance *foreignBackplane) String() string { return instance.label }

type hubHolder struct {
    hub *foreignHub
    ctx context.Context
}

func (instance *hubHolder) Close() error { return nil }

/* the hub is named by a pointer the walk reads whole; what the hub holds through an INTERFACE is two words somebody else is replacing, and the walk does not read them. Run under -race, the earlier walk that did reported the race on every run; this one reports none, and the assertion below is what pins the same thing without the detector. */
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
        identities := heldPointerIdentities(&hubHolder{hub: hub}, false)

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

/* a context is held through the holder's OWN interface field, so the pointer behind it is read and named; what that pointer names — the runtime's cancel context, with the done channel stored atomically on first use — is foreign memory, and nothing inside it is read. */
func TestHeldPointerIdentities_DoesNotReadInsideAContextItHolds(t *testing.T) {
    for iteration := 0; iteration < 500; iteration = iteration + 1 {
        ctx, cancel := context.WithCancel(context.Background())

        var firstUse sync.WaitGroup
        firstUse.Add(1)

        go func() {
            defer firstUse.Done()

            _ = ctx.Done()
        }()

        identities := heldPointerIdentities(&hubHolder{ctx: ctx}, false)

        firstUse.Wait()
        cancel()

        if 2 > len(identities) {
            t.Fatalf("expected the holder and the context pointer, got %d identities", len(identities))
        }

        /* what the walk must not reach is anything BENEATH the context: its pointer is the holder's own interface field, and what that pointer names is the runtime's; a later runtime may lay the context out differently, so the assertion is on what is read, not on a count */
        for _, held := range identities {
            if false == strings.HasPrefix(held.identity.valueType.String(), "*context.") && reflect.TypeOf((*hubHolder)(nil)) != held.identity.valueType {
                t.Fatalf("expected nothing beneath the context to be read, got %s", held.identity.valueType)
            }
        }
    }
}

/* the collaborator behind an intermediate struct the container does not know is still found: the walk enters foreign memory, it only refuses to read what a torn read could fabricate a type from. */
type intermediateStruct struct{ service *closeOrderServiceB }

type outerHolder struct{ middle *intermediateStruct }

func (instance *outerHolder) Close() error { return nil }

func TestHeldPointerIdentities_StillFindsAServiceBehindAnIntermediateStruct(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    if false == holdsIdentity(heldPointerIdentities(&outerHolder{middle: &intermediateStruct{service: held}}, false), heldIdentity) {
        t.Fatalf("expected the service behind an intermediate struct pointer to be found")
    }
}

/* chainLink and the two roots reach the SAME tail by a long path and by a short one, in both field orders. A pointer first met at the depth limit is entered again from the shorter path, so the tail's service is found whichever field the struct lists first. Five links put the tail exactly at the limit: the walk spends two levels per link. */
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

        if false == holdsIdentity(heldPointerIdentities(&chainFirstRoot{chain: chainOfLinks(length, tail), direct: tail}, false), heldIdentity) {
            t.Fatalf("expected the service to be found with the long path listed first, chain of %d links", length)
        }

        if false == holdsIdentity(heldPointerIdentities(&directFirstRoot{chain: chainOfLinks(length, tail), direct: tail}, false), heldIdentity) {
            t.Fatalf("expected the service to be found with the short path listed first, chain of %d links", length)
        }
    }
}

/* wideHolder nests two arrays at the per-level limit, which is more cells than the walk's whole budget: a service in the first cell is found and one in the last is not, which is the budget doing what the per-level limits cannot. */
type wideHolder struct {
    cells [256][256]struct{ service *closeOrderServiceB }
}

func (instance *wideHolder) Close() error { return nil }

func TestHeldPointerIdentities_StopsAtTheNodeBudget(t *testing.T) {
    held := &closeOrderServiceB{}
    heldIdentity, _ := pointerKeyOf(held)

    first := &wideHolder{}
    first.cells[0][0].service = held

    if false == holdsIdentity(heldPointerIdentities(first, false), heldIdentity) {
        t.Fatalf("expected a service in the first cell to be found inside the budget")
    }

    last := &wideHolder{}
    last.cells[255][255].service = held

    if true == holdsIdentity(heldPointerIdentities(last, false), heldIdentity) {
        t.Fatalf("expected the walk to stop at its budget before the last cell")
    }
}

/* one value filed under its name and under its type is walked once; the two nodes read the same record. */
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

/* the record keeps the collaborator ALIVE: a holder that drops its collaborator after construction would otherwise leave a bare address behind, and the next allocation of that size class would be matched as the service the holder never held. */
func TestHeldPointerIdentities_KeepsAHeldCollaboratorAlive(t *testing.T) {
    finalized := make(chan struct{}, 1)

    holder := &capturingHolder{held: &closeOrderServiceB{}}
    runtime.SetFinalizer(holder.held, func(*closeOrderServiceB) { finalized <- struct{}{} })

    identities := heldPointerIdentities(holder, false)
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

/* ringNode holds the next node of a ring. A ring longer than the walk's reach is one inferred edge per link and no ordering, and the pair rule alone leaves it standing — measured, eight nodes reported a cycle over a teardown in which all eight closed. */
type ringNode struct {
    next     *ringNode
    recorder *closeOrderRecorder
    label    string
}

func (instance *ringNode) Close() error {
    instance.recorder.record(instance.label)

    return nil
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

/* resolvingHolder resolves its collaborator, so the container itself writes the edge, and keeps no pointer to it; the collaborator holds the holder back. The inferred edge contradicts the resolved one and is not written: the pair closes in the resolved order and reports nothing. */
type resolvingHolder struct {
    recorder *closeOrderRecorder
}

func (instance *resolvingHolder) Close() error {
    instance.recorder.record("holder")

    return nil
}

type backHolder struct {
    recorder *closeOrderRecorder
    back     *resolvingHolder
}

func (instance *backHolder) Close() error {
    instance.recorder.record("collaborator")

    return nil
}

func TestContainer_Close_ArmedAHeldEdgeAgainstAResolvedEdgeIsNotWritten(t *testing.T) {
    serviceContainer := NewContainer()

    armParallelTeardown(t, serviceContainer)

    var mutex sync.Mutex
    closeSequence := make([]string, 0, 2)
    recorder := &closeOrderRecorder{mutex: &mutex, closeSequence: &closeSequence}

    /* the holder exists before either provider runs, so the collaborator HOLDS it from its own construction — which is when the walk reads it — and the holder's provider then resolves the collaborator, which is the edge the container writes itself. The holder does NOT keep what it resolved: kept, the pair would hold each other and the pair rule alone would drop both inferences, and this test would pass without ever asking the inference against the resolution — measured, it did */
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

/* a slice is a header of three words a concurrent append replaces, so a slice inside foreign memory is not read either; the value's OWN slices are, since nothing is writing them yet. */
type foreignBag struct{ items []*closeOrderServiceB }

type bagHolder struct {
    bag   *foreignBag
    items []*closeOrderServiceB
}

func (instance *bagHolder) Close() error { return nil }

func TestHeldPointerIdentities_ReadsItsOwnSlicesAndNotASliceOfForeignMemory(t *testing.T) {
    own := &closeOrderServiceB{}
    ownIdentity, _ := pointerKeyOf(own)

    foreign := &closeOrderServiceB{}
    foreignIdentity, _ := pointerKeyOf(foreign)

    identities := heldPointerIdentities(&bagHolder{bag: &foreignBag{items: []*closeOrderServiceB{foreign}}, items: []*closeOrderServiceB{own}}, false)

    if false == holdsIdentity(identities, ownIdentity) {
        t.Fatalf("expected a service in the holder's own slice to be found")
    }

    if true == holdsIdentity(identities, foreignIdentity) {
        t.Fatalf("expected a slice inside foreign memory to stay unread")
    }
}

/* what arming walks has been published — it is in use, its owner may be writing it — so it is walked as foreign memory, its own interface fields included: a hub built BEFORE arming, whose backplane its owner swaps under its own mutex, is named by its pointer and what its interface field holds stays unread. The walk that read it as the value's own memory reported the race under the detector on every run, and the example application arms after a boot that has built services by then. */
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

    for iteration := 0; iteration < 200; iteration = iteration + 1 {
        armed := serviceContainer.(*container)
        armed.mutex.Lock()
        armed.recordHeldIdentitiesOfBuiltServicesLocked()
        armed.mutex.Unlock()
    }

    armParallelTeardown(t, serviceContainer)

    close(stop)
    writes.Wait()

    for _, held := range serviceContainer.(*container).heldIdentitiesByNodeKey[containerNameNodeKey("app.hub")] {
        if reflect.TypeOf((*foreignBackplane)(nil)) == held.identity.valueType {
            t.Fatalf("expected the backplane behind a built service's interface field to stay unread at arming")
        }
    }
}

/* aliasedResolvingService is resolved through its TYPE, so it lives under two nodes — its name and its type — and keeps no pointer to what it resolves, so the only way from it to its collaborator is the resolved edge, filed under one of its two keys; aliasedCapturingService holds it back through a captured pointer. */
type aliasedResolvingService struct {
    recorder *closeOrderRecorder
}

func (instance *aliasedResolvingService) Close() error {
    instance.recorder.record("resolving")

    return nil
}

type aliasedCapturingService struct {
    recorder *closeOrderRecorder
    back     *aliasedResolvingService
}

func (instance *aliasedCapturingService) Close() error {
    instance.recorder.record("capturing")

    return nil
}

/* the ring an inferred edge would close has to be looked for in the CANONICAL key space, where a service filed under its name and under its type is one node: asked on the raw keys, the resolved edge from the type node and the held edge onto the name node never met, the held edge was written, and the drain — which folds the aliases first — reported a cycle over a teardown in which both services closed, 29 times out of 200, by map order. Fifty rounds here, because a defect that appears one time in seven does not show in one. */
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

/* the records the walk keeps hold every named collaborator alive for the plan; once the teardown has run they are released with it, so a value a service dropped after construction is collectable again. Before the release it is pinned, which is the control: the same holder, the same collection, the finalizer does not run until Close has. */
func TestContainer_Close_ReleasesTheHeldIdentityRecords(t *testing.T) {
    finalized := make(chan struct{}, 1)

    serviceContainer := NewContainer()

    /* the container has to outlive the collection below, or the record dies with the container and the test passes for that reason instead */
    defer runtime.KeepAlive(serviceContainer)

    armParallelTeardown(t, serviceContainer)

    holder := &capturingHolder{recorder: &closeOrderRecorder{mutex: &sync.Mutex{}, closeSequence: &[]string{}}, held: &closeOrderServiceB{}}
    runtime.SetFinalizer(holder.held, func(*closeOrderServiceB) { finalized <- struct{}{} })

    if registerErr := serviceContainer.Register(
        "app.holder",
        func(_ containercontract.Resolver) (*capturingHolder, error) { return holder, nil },
        WithoutTypeRegistration(),
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.holder"); nil != getErr {
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

    if closeErr := serviceContainer.Close(); nil != closeErr {
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

/* an inference is this plan's, not the graph's: the operator's view computes a plan too, and an inference it wrote into the graph outlived the state it was drawn from — a lazy resolution made after the view in the opposite direction then closed a ring nothing had inferred, measured twenty times out of twenty. */
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

    /* the operator's view, computed BEFORE the late resolution: it infers holder → held */
    serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan()

    /* the held service, through the resolver it kept, resolves the holder: held → holder, which contradicts the inference and must win */
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

/* the way back that decides whether an inference would close a ring runs through the nodes the drain will read — the CREATED ones — so a declared edge towards a service nobody built is not a way back: it dropped a true inference and left the pair in one wave. */
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

    for _, entry := range entries {
        if "service:app.held" == entry.NodeKey && 1 != entry.WaveIndex {
            t.Fatalf("expected the held service one wave past its holder, got %+v", entries)
        }
    }
}
