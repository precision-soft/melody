package application

import (
    "context"
    "errors"
    "io"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
)

func TestApplicationClose_AnApplicationWithoutAKernelHasNothingToTearDown(t *testing.T) {
    applicationInstance := &Application{
        runtimeFlags: NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected a kernel-less application to close cleanly, got %v", closeErr)
    }

    applicationInstance.CloseWithContext(context.Background())
}

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

func TestApplicationClose_AFailureSomebodyElseAlreadyCarriedAwayIsNotReportedAgain(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    registerErr := serviceContainer.Register(
        "app.failing.close",
        func(resolver containercontract.Resolver) (*failingCloseProbe, error) {
            return &failingCloseProbe{}, nil
        },
        containercontract.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.failing.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    firstCloseErr := serviceContainer.CloseWithContext(context.Background())
    if nil == firstCloseErr {
        t.Fatalf("expected the failing service to fail the first close")
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected the teardown to leave a failure it did not discover to its discoverer, got %v", closeErr)
    }
}

func TestApplicationClose_AFailureItDiscoversItselfTravelsOut(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    registerErr := serviceContainer.Register(
        "app.failing.close",
        func(resolver containercontract.Resolver) (*failingCloseProbe, error) {
            return &failingCloseProbe{}, nil
        },
        containercontract.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.failing.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    closeErr := applicationInstance.close(context.Background())
    if nil == closeErr {
        t.Fatalf("expected the teardown to report the failure it discovered")
    }
}

type failingCloseProbe struct{}

func (instance *failingCloseProbe) CloseWithContext(closeContext context.Context) error {
    return errFailingCloseProbe
}

var errFailingCloseProbe = errors.New("the service refused to close")

func TestApplicationClose_ATypedNilKernelHasNothingToTearDown(t *testing.T) {
    applicationInstance := &Application{
        kernel:       (*testKernel)(nil),
        runtimeFlags: NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected a typed-nil kernel to close cleanly, got %v", closeErr)
    }
}

func TestApplicationClose_ConcurrentClosesReportOneFailureOnce(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    registerErr := serviceContainer.Register(
        "app.failing.close",
        func(resolver containercontract.Resolver) (*failingCloseProbe, error) {
            return &failingCloseProbe{}, nil
        },
        containercontract.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.failing.close"); nil != getErr {
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

type gatedCloseProbe struct {
    entered chan struct{}
    release chan struct{}
}

func (instance *gatedCloseProbe) CloseWithContext(closeContext context.Context) error {
    close(instance.entered)
    <-instance.release

    return nil
}

func TestApplicationClose_ASiblingCloseWaitsForThePerformersTeardown(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    probe := &gatedCloseProbe{entered: make(chan struct{}), release: make(chan struct{})}

    serviceContainer := kernelInstance.ServiceContainer()

    registerErr := serviceContainer.Register(
        "app.gated.close",
        func(resolver containercontract.Resolver) (*gatedCloseProbe, error) {
            return probe, nil
        },
        containercontract.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.gated.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    performerDone := make(chan error, 1)
    go func() {
        performerDone <- applicationInstance.close(context.Background())
    }()

    <-probe.entered

    siblingDone := make(chan error, 1)
    go func() {
        siblingDone <- applicationInstance.close(context.Background())
    }()

    select {
    case <-siblingDone:
        t.Fatalf("expected the sibling close to wait while the performer's teardown is open")
    case <-time.After(50 * time.Millisecond):
    }

    close(probe.release)

    if performerErr := <-performerDone; nil != performerErr {
        t.Fatalf("expected the performer to close cleanly, got %v", performerErr)
    }

    if siblingErr := <-siblingDone; nil != siblingErr {
        t.Fatalf("expected the sibling to report nothing, got %v", siblingErr)
    }
}

type overrunSleeper struct {
    sleep time.Duration
}

func (instance *overrunSleeper) CloseWithContext(closeContext context.Context) error {
    time.Sleep(instance.sleep)

    return nil
}

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

func TestApplicationClose_AnOverrunTheContainerReportsIsWrittenAsAWarningAndReturnsNil(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    if registerErr := serviceContainer.Register(
        "app.eater",
        func(_ containercontract.Resolver) (*overrunSleeper, error) { return &overrunSleeper{sleep: 80 * time.Millisecond}, nil },
        containercontract.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.eater"); nil != getErr {
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

func TestApplicationClose_AnOverrunSomebodyElseAlreadyCarriedAwayIsNotReportedAgain(t *testing.T) {
    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    if registerErr := serviceContainer.Register(
        "app.eater",
        func(_ containercontract.Resolver) (*overrunSleeper, error) { return &overrunSleeper{sleep: 80 * time.Millisecond}, nil },
        containercontract.RegisterOptions{},
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get(t.Context(), "app.eater"); nil != getErr {
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

type teardownDeadlineProbe struct {
    waitsForTheDeadline bool
    deadline            time.Time
    hasDeadline         bool
}

func (instance *teardownDeadlineProbe) CloseWithContext(closeContext context.Context) error {
    instance.deadline, instance.hasDeadline = closeContext.Deadline()

    if true == instance.waitsForTheDeadline {
        <-closeContext.Done()

        return closeContext.Err()
    }

    return nil
}

func bootApplicationWithTeardownProbe(t *testing.T, teardownTimeout bound.Duration, probe *teardownDeadlineProbe) *Application {
    t.Helper()

    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), TeardownTimeout: teardownTimeout})
    applicationInstance.RegisterService("app.teardown.probe", func(resolver containercontract.Resolver) (*teardownDeadlineProbe, error) {
        return probe, nil
    }, containercontract.RegisterOptions{BuiltAtBoot: true, BootFailure: containercontract.BootFailureFailsBoot})
    applicationInstance.Boot()

    return applicationInstance
}

func TestApplication_CloseWithContextAppliesTheTeardownTimeout(t *testing.T) {
    bounded := &teardownDeadlineProbe{}
    if closeErr := bootApplicationWithTeardownProbe(t, bound.Of(40*time.Second), bounded).CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if remaining := time.Until(bounded.deadline); false == bounded.hasDeadline || remaining > 40*time.Second || remaining < 35*time.Second {
        t.Fatalf("expected the teardown timeout to bound a close the caller set no deadline on, got %v (has deadline: %v)", remaining, bounded.hasDeadline)
    }

    shorter := &teardownDeadlineProbe{}
    callerContext, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    if closeErr := bootApplicationWithTeardownProbe(t, bound.Of(40*time.Second), shorter).CloseWithContext(callerContext); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if remaining := time.Until(shorter.deadline); false == shorter.hasDeadline || remaining > time.Second {
        t.Fatalf("expected the caller's shorter deadline to end the close, got %v", remaining)
    }

    unlimited := &teardownDeadlineProbe{}
    if closeErr := bootApplicationWithTeardownProbe(t, bound.UnlimitedDuration(), unlimited).CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if true == unlimited.hasDeadline {
        t.Fatalf("expected no deadline under an unlimited teardown timeout, got %v", unlimited.deadline)
    }
}

func TestApplication_ACloseEndedByTheTeardownTimeoutNamesIt(t *testing.T) {
    probe := &teardownDeadlineProbe{waitsForTheDeadline: true}

    closeErr := bootApplicationWithTeardownProbe(t, bound.Of(30*time.Millisecond), probe).CloseWithContext(context.Background())
    if nil == closeErr || config.TeardownTimeoutKey != exception.FromError(closeErr).Context()["bound"] {
        t.Fatalf("expected the close to name the teardown timeout, got %v", closeErr)
    }
}


func TestApplicationClose_AServiceWhoseTeardownWalkTheBudgetEndedIsNamedInAWarning(t *testing.T) {
    for _, testCase := range []struct {
        name   string
        nodes  bound.Count
        warned bool
    }{
        {"the default budget", bound.Count{}, false},
        {"a budget of two items", bound.Of(2), true},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            kernelInstance := newTestKernel()
            kernelInstance.serviceContainer = container.NewContainer(container.ContainerConfig{TeardownWalk: container.TeardownWalkConfig{Nodes: testCase.nodes}})
            applicationInstance := newScopedServiceApplication(kernelInstance)

            serviceContainer := kernelInstance.ServiceContainer()

            if registerErr := serviceContainer.Register(
                "app.holder",
                func(_ containercontract.Resolver) (*teardownWalkHolder, error) {
                    return &teardownWalkHolder{first: &teardownWalkHolder{first: &teardownWalkHolder{}}}, nil
                },
                containercontract.RegisterOptions{},
            ); nil != registerErr {
                t.Fatalf("unexpected register error: %v", registerErr)
            }

            if _, getErr := serviceContainer.Get(t.Context(), "app.holder"); nil != getErr {
                t.Fatalf("unexpected get error: %v", getErr)
            }

            written := captureEmergencyLogger(t, func() {
                if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
                    t.Fatalf("unexpected close error: %v", closeErr)
                }
            })

            warned := strings.Contains(written, TeardownWalkBudgetEndedMessage) && strings.Contains(written, "app.holder") && strings.Contains(written, "ContainerConfig.TeardownWalk.Nodes")
            if testCase.warned != warned {
                t.Fatalf("expected the warning written %v, got %q", testCase.warned, written)
            }
        })
    }
}
