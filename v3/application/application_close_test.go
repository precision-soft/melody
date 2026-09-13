package application

import (
    "context"
    "errors"
    "io"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/config"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
)

/* Close is what a boot that died halfway and a clean shutdown both reach, and neither shape had ever been driven: an application assembled without a kernel — the state a boot failure leaves — has nothing to tear down, and dereferencing the absent kernel there would replace a clean exit with a panic inside the one handler that must not panic. */
func TestApplicationClose_AnApplicationWithoutAKernelHasNothingToTearDown(t *testing.T) {
    applicationInstance := &Application{
        runtimeFlags: NewRuntimeFlags(config.ModeHttp),
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected a kernel-less application to close cleanly, got %v", closeErr)
    }

    /* the exported form swallows the result on purpose; what it must not do is panic */
    applicationInstance.Close()
}

/* the teardown closes the container the kernel holds, and it has to be idempotent: the cli action closes the container right after the command runs, and the deferred Close arrives afterwards. */
func TestApplicationClose_ClosesTheContainerAndStaysIdempotent(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    closedChecker, isChecker := kernelInstance.ServiceContainer().(interface{ IsClosed() bool })
    if false == isChecker {
        t.Fatalf("expected the container to report closedness")
    }

    if true == closedChecker.IsClosed() {
        t.Fatalf("expected the container to start open")
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == closedChecker.IsClosed() {
        t.Fatalf("expected the teardown to close the container")
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected a second close to stay quiet, got %v", closeErr)
    }
}

/* a container SOMEBODY ELSE already closed hands its memoized failure to every later Close, and re-reporting it here would present one incident as two — the exit code of that failure already belongs to whoever performed the close, which folded it into its own result. The teardown reports only the failure IT discovered. */
func TestApplicationClose_AFailureSomebodyElseAlreadyCarriedAwayIsNotReportedAgain(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    registerErr := serviceContainer.Register(
        "app.failing.close",
        func(resolver containercontract.Resolver) (*failingCloseProbe, error) {
            return &failingCloseProbe{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.failing.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    /* the first close is somebody else's — the shape the cli action leaves behind */
    firstCloseErr := serviceContainer.Close()
    if nil == firstCloseErr {
        t.Fatalf("expected the failing service to fail the first close")
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected the teardown to leave a failure it did not discover to its discoverer, got %v", closeErr)
    }
}

/* the failure the teardown DID discover has to travel out of it, because the exit code of the process is built from it — a shutdown that lost a connection back to nobody used to exit zero. */
func TestApplicationClose_AFailureItDiscoversItselfTravelsOut(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    registerErr := serviceContainer.Register(
        "app.failing.close",
        func(resolver containercontract.Resolver) (*failingCloseProbe, error) {
            return &failingCloseProbe{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.failing.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := applicationInstance.close(context.Background())
    if nil == closeErr {
        t.Fatalf("expected the teardown to report the failure it discovered")
    }
}

type failingCloseProbe struct{}

func (instance *failingCloseProbe) Close() error {
    return errFailingCloseProbe
}

var errFailingCloseProbe = errors.New("the service refused to close")

/* the same absence reached through the interface: a typed-nil kernel passes a plain comparison and reaches ServiceContainer() on a nil receiver, inside the before-exit hook of the one handler that must not panic */
func TestApplicationClose_ATypedNilKernelHasNothingToTearDown(t *testing.T) {
    applicationInstance := &Application{
        kernel:       (*testKernel)(nil),
        runtimeFlags: NewRuntimeFlags(config.ModeHttp),
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected a typed-nil kernel to close cleanly, got %v", closeErr)
    }
}

/* two closes racing each other must present one teardown failure as one incident: the container's closedness probe alone cannot decide, because both callers read it open before either enters Close, and both then receive the memoized failure. The claim admits exactly one performer on every schedule, so the assertion that matters is the negative one — never two reports. */
func TestApplicationClose_ConcurrentClosesReportOneFailureOnce(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    registerErr := serviceContainer.Register(
        "app.failing.close",
        func(resolver containercontract.Resolver) (*failingCloseProbe, error) {
            return &failingCloseProbe{}, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.failing.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    startLine := make(chan struct{})
    results := make(chan error, 2)

    for index := 0; 2 > index; index++ {
        go func() {
            <-startLine
            results <- applicationInstance.close(context.Background())
        }()
    }

    close(startLine)

    reportedFailures := 0
    for index := 0; 2 > index; index++ {
        if closeErr := <-results; nil != closeErr {
            reportedFailures = reportedFailures + 1
        }
    }

    if 1 != reportedFailures {
        t.Fatalf("expected the one teardown failure to be reported exactly once, got %d reports", reportedFailures)
    }
}

/* overrunSleeper is the plain closer that eats a budget and answers nil: bounded by nothing but itself, and named by nobody until the deadline record */
type overrunSleeper struct {
    sleep time.Duration
}

func (instance *overrunSleeper) Close() error {
    time.Sleep(instance.sleep)

    return nil
}

/* captureEmergencyLogger hands back what the emergency logger wrote during a call: the logger is built lazily on os.Stderr, so it is closed before the call, os.Stderr is a pipe for its duration, and it is closed again after it so the lines are flushed and the next caller gets a logger on the real stderr */
func captureEmergencyLogger(t *testing.T, action func()) string {
    t.Helper()

    logging.CloseEmergencyLogger()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("unexpected pipe error: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
    }()

    action()

    logging.CloseEmergencyLogger()

    _ = writeEnd.Close()
    os.Stderr = originalStderr

    output, readErr := io.ReadAll(readEnd)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    return string(output)
}

/* a teardown that ran past its deadline and failed nothing returns nil — a spent budget is not a failure — and the record of who spent it reaches the journal as a warning, through the door the container keeps for exactly that close */
func TestApplicationClose_AnOverrunTheContainerReportsIsWrittenAsAWarningAndReturnsNil(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    if registerErr := serviceContainer.Register(
        "app.eater",
        func(_ containercontract.Resolver) (*overrunSleeper, error) { return &overrunSleeper{sleep: 80 * time.Millisecond}, nil },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.eater"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    var closeErr error

    written := captureEmergencyLogger(t, func() {
        closeContext, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
        defer cancel()

        closeErr = applicationInstance.close(closeContext)
    })

    if nil != closeErr {
        t.Fatalf("expected the overrun alone to return nil, got %v", closeErr)
    }

    if false == strings.Contains(written, "service container teardown overran its deadline") || false == strings.Contains(written, `"warning"`) {
        t.Fatalf("expected the overrun written as a warning, got %q", written)
    }

    if false == strings.Contains(written, "service:app.eater") || false == strings.Contains(written, `"spentBy"`) {
        t.Fatalf("expected the record to name the closer that spent the budget, got %q", written)
    }
}

/* the record belongs to whoever performed the close: a container somebody else already closed is not reported again here, the rule the failure above follows */
func TestApplicationClose_AnOverrunSomebodyElseAlreadyCarriedAwayIsNotReportedAgain(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    if registerErr := serviceContainer.Register(
        "app.eater",
        func(_ containercontract.Resolver) (*overrunSleeper, error) { return &overrunSleeper{sleep: 80 * time.Millisecond}, nil },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.eater"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeContext, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
    defer cancel()

    if closeErr := serviceContainer.(interface {
        CloseWithContext(context.Context) error
    }).CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    written := captureEmergencyLogger(t, func() {
        if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
            t.Fatalf("expected the second close to stay quiet, got %v", closeErr)
        }
    })

    if true == strings.Contains(written, "overran") {
        t.Fatalf("expected the overrun somebody else carried away not to be reported again, got %q", written)
    }
}
