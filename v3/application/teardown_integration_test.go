package application

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "sync/atomic"
    "testing"
    "testing/fstest"
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
)

type applicationTeardownProbe struct {
    close func(context.Context) error
}

func (instance *applicationTeardownProbe) Close() error {
    return errors.New("plain Close was selected instead of CloseWithContext")
}

func (instance *applicationTeardownProbe) CloseWithContext(ctx context.Context) error {
    return instance.close(ctx)
}

/* Exercise Boot, the configured budget, CLI Run and its deferred teardown together. */
func TestRun_UsesConfiguredBudgetAndArmedDependencyWaves(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"probe", "probe:serving"}
    t.Cleanup(func() { os.Args = originalArguments })

    originalExit := applicationExit
    var exitCode atomic.Int32
    applicationExit = func(code int) { exitCode.Store(int32(code)) }
    t.Cleanup(func() { applicationExit = originalExit })

    directory := t.TempDir()
    environment := []byte("MELODY_TEARDOWN_TIMEOUT=3s\n")
    if err := os.WriteFile(filepath.Join(directory, ".env"), environment, 0o600); nil != err {
        t.Fatal(err)
    }
    previousDirectory, err := os.Getwd()
    if nil != err {
        t.Fatal(err)
    }
    if err := os.Chdir(directory); nil != err {
        t.Fatal(err)
    }
    t.Cleanup(func() { _ = os.Chdir(previousDirectory) })
    embeddedEnvironment := fstest.MapFS{".env": &fstest.MapFile{Data: environment}}
    app := NewApplication(context.Background(), embeddedEnvironment, testhelper.NewEmbeddedStaticFs())
    command := &servingProbeApplicationCommand{}
    app.RegisterCliCommand(command)

    entered := make(chan context.Context, 2)
    release := make(chan struct{})
    var completed atomic.Int32
    var dependencyClosed atomic.Bool
    dependency := &applicationTeardownProbe{close: func(ctx context.Context) error {
        if 2 != completed.Load() {
            return errors.New("dependency closed before its dependents finished")
        }
        dependencyClosed.Store(true)
        return nil
    }}
    app.RegisterService("probe.dependency", func(containercontract.Resolver) (*applicationTeardownProbe, error) { return dependency, nil }, container.WithoutTypeRegistration())
    for _, name := range []string{"probe.first", "probe.second"} {
        probe := &applicationTeardownProbe{close: func(ctx context.Context) error {
            entered <- ctx
            select {
            case <-release:
                completed.Add(1)
                return nil
            case <-ctx.Done():
                return ctx.Err()
            }
        }}
        app.RegisterService(name, func(containercontract.Resolver) (*applicationTeardownProbe, error) { return probe, nil }, container.WithoutTypeRegistration(), container.WithTeardownDependency("probe.dependency"))
    }

    services := app.Boot().ServiceContainer()
    for _, name := range []string{"probe.dependency", "probe.first", "probe.second"} {
        if _, err := services.Get(name); nil != err {
            t.Fatal(err)
        }
    }
    armable := services.(interface{ ArmParallelTeardown() error })
    if err := armable.ArmParallelTeardown(); nil != err {
        t.Fatal(err)
    }

    done := make(chan struct{})
    go func() {
        defer close(done)
        app.Run()
    }()

    timer := time.NewTimer(5 * time.Second)
    defer timer.Stop()
    contexts := make([]context.Context, 0, 2)
    for len(contexts) < 2 {
        select {
        case ctx := <-entered:
            contexts = append(contexts, ctx)
        case <-timer.C:
            close(release)
            <-done
            t.Fatal("both independent closers must start before either is released")
        }
    }
    close(release)
    select {
    case <-done:
    case <-timer.C:
        t.Fatal("application teardown did not finish")
    }

    if false == command.ran || 0 != exitCode.Load() || false == dependencyClosed.Load() {
        t.Fatalf("run=%v exit=%d dependencyClosed=%v", command.ran, exitCode.Load(), dependencyClosed.Load())
    }
    first, hasFirst := contexts[0].Deadline()
    second, hasSecond := contexts[1].Deadline()
    if false == hasFirst || false == hasSecond || false == first.Equal(second) {
        t.Fatalf("closers did not share one deadline: %v/%v, %v/%v", first, hasFirst, second, hasSecond)
    }
    if remaining := time.Until(first); 0 >= remaining || 3*time.Second < remaining {
        t.Fatalf("deadline does not reflect the configured three-second budget: %v", remaining)
    }
}
