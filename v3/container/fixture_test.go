package container

import (
    "context"
    "errors"
    "strings"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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

/* scopedCloseRecorder records the order its services were closed in, which is the only way to observe that a teardown honoured the dependency graph rather than the node names. */
type scopedCloseRecorder struct {
    mutex sync.Mutex
    order []string
}

func (instance *scopedCloseRecorder) record(name string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.order = append(instance.order, name)
}

func (instance *scopedCloseRecorder) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    copied := make([]string, len(instance.order))
    copy(copied, instance.order)

    return copied
}

type recordingScopedService struct {
    name     string
    recorder *scopedCloseRecorder
}

func (instance *recordingScopedService) Close() error {
    instance.recorder.record(instance.name)

    return nil
}

type contextClosingScopedProbe struct {
    plainCalls int
    contexts   []context.Context
}

func (instance *contextClosingScopedProbe) Close() error {
    instance.plainCalls++

    return nil
}

func (instance *contextClosingScopedProbe) CloseWithContext(closeContext context.Context) error {
    instance.contexts = append(instance.contexts, closeContext)

    return nil
}

type reflectionGraphNode struct {
    left  *reflectionGraphNode
    right *reflectionGraphNode
    value int
}

func (instance *reflectionGraphNode) Close() error {
    return nil
}

type closeOrderRecorder struct {
    mutex         *sync.Mutex
    closeSequence *[]string
}

func (instance *closeOrderRecorder) record(value string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    *instance.closeSequence = append(*instance.closeSequence, value)
}

type closeOrderServiceA struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceA) Close() error {
    instance.recorder.record("a")
    return nil
}

type closeOrderServiceB struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceB) Close() error {
    instance.recorder.record("b")
    return nil
}

type closeOrderServiceC struct {
    recorder *closeOrderRecorder
}

func (instance *closeOrderServiceC) Close() error {
    instance.recorder.record("c")
    return nil
}

type panickingCloseWithCauseService struct {
    cause error
}

func (instance *panickingCloseWithCauseService) Close() error {
    panic(instance.cause)
}

/* assertCloseFailureDetails reads the details a teardown error carries for one failed node: the line in the failure map, and beside it the recovered value and the frames of the contained panic. */
func assertCloseFailureDetails(t *testing.T, context exceptioncontract.Context, nodeKey string) {
    t.Helper()

    failures, hasFailures := context["failures"].(map[string]string)
    if false == hasFailures || "service close panicked" != failures[nodeKey] {
        t.Fatalf("expected the failure line under %q, got %v", nodeKey, context["failures"])
    }

    failureDetails, hasDetails := context["failureDetails"].(map[string]exceptioncontract.Context)
    if false == hasDetails {
        t.Fatalf("expected the failure details beside the failure map, got %v", context["failureDetails"])
    }

    details := failureDetails[nodeKey]
    if "the drain buffer was nil" != details["recoveredValue"] {
        t.Fatalf("expected the recovered value under %q, got %v", nodeKey, details)
    }

    panicStack, hasStack := details["panicStack"].(string)
    if false == hasStack || false == strings.Contains(panicStack, "panickingCloseWithCauseService") {
        t.Fatalf("expected the frames that ran under %q, got %v", nodeKey, details["panicStack"])
    }
}

type panickingUnwrapCloseService struct{}

func (instance *panickingUnwrapCloseService) Close() error {
    return panickingUnwrapError{}
}

type sharedPoolService struct {
    label string
}

func (instance *sharedPoolService) Close() error { return nil }

type poolDeclarerService struct {
    label string
}

func (instance *poolDeclarerService) Close() error { return nil }

type registerScopedProbe struct {
    value string
}

type registerScopedOtherProbe struct {
    value string
}

type testService struct {
    Value string
}

type testInterface interface {
    Name() string
}

type testImplementation struct {
    name string
}

func (instance *testImplementation) Name() string {
    return instance.name
}

type providerContractProbe struct {
    value string
}

/* renderedCauseChain walks the whole chain because a provider panic is wrapped by the creation guard before it reaches the caller, and only the chain says what the provider itself refused. */
func renderedCauseChain(err error) string {
    rendered := ""
    for current := err; nil != current; current = errors.Unwrap(current) {
        rendered = rendered + current.Error() + "\n"
    }

    return rendered
}

type collectableHandler interface {
    Handle() string
}

type unrelatedContract interface {
    Unrelated()
}

type invoiceHandler struct {
}

func (instance *invoiceHandler) Handle() string {
    return "invoice"
}

type auditHandler struct {
}

func (instance *auditHandler) Handle() string {
    return "audit"
}

func newCollectionContainer(t *testing.T) containercontract.Container {
    t.Helper()

    serviceContainer := NewContainer()

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*invoiceHandler, error) {
        return &invoiceHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*auditHandler, error) {
        return &auditHandler{}, nil
    })

    MustRegisterType(serviceContainer, func(resolver containercontract.Resolver) (*plainService, error) {
        return &plainService{}, nil
    })

    return serviceContainer
}

type scopeTestService struct {
    value string
}

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

type fitsProbeGreeter interface {
    Greet() string
}

type fitsProbeImplementer struct{}

func (instance *fitsProbeImplementer) Greet() string {
    return "fits"
}
