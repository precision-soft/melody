package application

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/config"
    containercontract "github.com/precision-soft/melody/container/contract"
)

/* @info Close is what a boot that died halfway and a clean shutdown both reach, and neither shape had ever been driven: an application assembled without a kernel — the state a boot failure leaves — has nothing to tear down, and dereferencing the absent kernel there would replace a clean exit with a panic inside the one handler that must not panic. */
func TestApplicationClose_AnApplicationWithoutAKernelHasNothingToTearDown(t *testing.T) {
    applicationInstance := &Application{
        runtimeFlags: NewRuntimeFlags(config.ModeHttp),
    }

    if closeErr := applicationInstance.close(); nil != closeErr {
        t.Fatalf("expected a kernel-less application to close cleanly, got %v", closeErr)
    }

    /* the exported form swallows the result on purpose; what it must not do is panic */
    applicationInstance.Close()
}

/* @info the teardown closes the container the kernel holds, and it has to be idempotent: the cli action closes the container right after the command runs, and the deferred Close arrives afterwards. */
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

    if closeErr := applicationInstance.close(); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if false == closedChecker.IsClosed() {
        t.Fatalf("expected the teardown to close the container")
    }

    if closeErr := applicationInstance.close(); nil != closeErr {
        t.Fatalf("expected a second close to stay quiet, got %v", closeErr)
    }
}

/* @info a container SOMEBODY ELSE already closed hands its memoized failure to every later Close, and re-reporting it here would present one incident as two — the exit code of that failure already belongs to whoever performed the close, which folded it into its own result. The teardown reports only the failure IT discovered. */
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

    if closeErr := applicationInstance.close(); nil != closeErr {
        t.Fatalf("expected the teardown to leave a failure it did not discover to its discoverer, got %v", closeErr)
    }
}

/* @info the failure the teardown DID discover has to travel out of it, because the exit code of the process is built from it — a shutdown that lost a connection back to nobody used to exit zero. */
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

    closeErr := applicationInstance.close()
    if nil == closeErr {
        t.Fatalf("expected the teardown to report the failure it discovered")
    }
}

type failingCloseProbe struct{}

func (instance *failingCloseProbe) Close() error {
    return errFailingCloseProbe
}

var errFailingCloseProbe = errors.New("the service refused to close")
