package cache

import (
    "context"
    "errors"
    "testing"
    "time"

    melodybound "github.com/precision-soft/melody/v4/bound"
    "github.com/redis/rueidis"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodycache "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type recordingRegistrar struct {
    names []string
}

func (instance *recordingRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.names = append(instance.names, serviceName)
}

func TestRegisterBackendServiceUsesCoreBackendName(t *testing.T) {
    registrar := &recordingRegistrar{}

    registerBackendService(registrar, BackendConfig{Prefix: "example"})

    if 0 == len(registrar.names) || melodycache.ServiceCacheBackend != registrar.names[0] {
        t.Fatalf("expected %q to be registered, got %v", melodycache.ServiceCacheBackend, registrar.names)
    }
}

type teardownSpyClient struct {
    rueidis.Client
    closed bool
}

func (instance *teardownSpyClient) Close() {
    instance.closed = true
}

type containerRegistrar struct {
    target containercontract.Container
}

func (instance *containerRegistrar) RegisterService(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.target.MustRegister(serviceName, provider, options)
}

func (instance *containerRegistrar) Register(serviceName string, provider any, options containercontract.RegisterOptions) error {
    return instance.target.Register(serviceName, provider, options)
}

func (instance *containerRegistrar) MustRegister(serviceName string, provider any, options containercontract.RegisterOptions) {
    instance.target.MustRegister(serviceName, provider, options)
}

func TestRegisterBackendService_ResolutionOrdersTheConnectionIntoTheTeardown(t *testing.T) {
    client := &teardownSpyClient{}
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}

    registerConnectionOver(serviceContainer, client)
    registerBackendService(registrar, BackendConfig{Prefix: "prefix"})

    backend := container.MustFromResolver[cachecontract.Backend](t.Context(), serviceContainer, melodycache.ServiceCacheBackend)
    if nil == backend {
        t.Fatalf("expected the backend to resolve")
    }

    if closeErr := serviceContainer.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("container close: %v", closeErr)
    }

    if false == client.closed {
        t.Fatalf("expected the resolved backend to pull the owning connection into the teardown")
    }
}

const registeredBackendProbeBudget = 2 * time.Second

func TestRegisterBackendService_HandsTheConfigToTheBackend(t *testing.T) {
    client, backendGate := dialGated(t)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}
    registerConnectionOver(serviceContainer, client)
    registerBackendService(registrar, BackendConfig{
        Prefix:         "melody:test:" + t.Name() + ":",
        ScanCount:      melodybound.Of(11),
        DeleteBatch:    melodybound.Of(13),
        MaxKeyLength:   melodybound.Of(7),
        CallTimeout:    melodybound.Of(50 * time.Millisecond),
    })

    service := container.MustFromResolver[*BackendService](t.Context(), serviceContainer, melodycache.ServiceCacheBackend)

    if 50*time.Millisecond != service.backend.callTimeout || 7 != service.backend.maxKeyLength || 11 != service.backend.scanCount || 13 != service.backend.deleteBatch {
        t.Fatalf("expected the config to reach the registered backend, got timeout %v, max key length %d, scan count %d and delete batch %d", service.backend.callTimeout, service.backend.maxKeyLength, service.backend.scanCount, service.backend.deleteBatch)
    }

    _ = service.Set(testRuntime(), "warm", []byte("x"), melodybound.UnlimitedDuration(), cachecontract.SetOptions{})
    _ = service.Delete(testRuntime(), "warm")

    backendGate.Wedge()

    getErr := awaitOutcome(t, registeredBackendProbeBudget, func() error {
        _, _, err := service.Get(testRuntime(), "key")

        return err
    })
    if nil == getErr || false == errors.Is(getErr, context.DeadlineExceeded) || false == errors.Is(getErr, ErrCallTimeout) {
        t.Fatalf("expected the registered backend's read to be refused with its own deadline, got %v", getErr)
    }
}

func TestRegisterBackendService_AnAbsentCallTimeoutRefusesAWedgedReadWithinOneSecond(t *testing.T) {
    client, backendGate := dialGated(t)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}
    registerConnectionOver(serviceContainer, client)
    registerBackendService(registrar, BackendConfig{Prefix: "melody:test:" + t.Name() + ":"})

    service := container.MustFromResolver[*BackendService](t.Context(), serviceContainer, melodycache.ServiceCacheBackend)

    _ = service.Set(testRuntime(), "warm", []byte("x"), melodybound.UnlimitedDuration(), cachecontract.SetOptions{})
    _ = service.Delete(testRuntime(), "warm")

    backendGate.Wedge()

    startedAt := time.Now()
    getErr := awaitOutcome(t, registeredBackendProbeBudget, func() error {
        _, _, err := service.Get(testRuntime(), "key")

        return err
    })
    if nil == getErr || false == errors.Is(getErr, context.DeadlineExceeded) || false == errors.Is(getErr, ErrCallTimeout) {
        t.Fatalf("expected the default-bounded read to be refused with its own deadline, got %v", getErr)
    }

    if elapsed := time.Since(startedAt); 900*time.Millisecond > elapsed {
        t.Fatalf("expected the refusal at the 1s default, got it after %v", elapsed)
    }
}

func TestRegisterBackendService_AnAbsentConfigBoundsEachRoundTripAtOneSecond(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registrar := &containerRegistrar{target: serviceContainer}
    registerConnectionOver(serviceContainer, &teardownSpyClient{})
    registerBackendService(registrar, BackendConfig{})

    service := container.MustFromResolver[*BackendService](t.Context(), serviceContainer, melodycache.ServiceCacheBackend)

    if time.Second != service.backend.callTimeout || "melody:cache:" != service.backend.prefix {
        t.Fatalf("expected the default 1s budget and prefix, got %v and %q", service.backend.callTimeout, service.backend.prefix)
    }
}

func TestRegisterBackendService_RefusesAtTheCall(t *testing.T) {
    cases := map[string]struct {
        config BackendConfig
        field  string
    }{
        "unlimited timeout": {config: BackendConfig{CallTimeout: melodybound.UnlimitedDuration()}, field: "BackendConfig.CallTimeout"},
        "zero scan count":   {config: BackendConfig{ScanCount: melodybound.Of(0)}, field: "BackendConfig.ScanCount"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            registrar := &recordingRegistrar{}

            func() {
                defer func() {
                    recoveredErr, isError := recover().(error)
                    if false == isError {
                        t.Fatal("expected the registration to panic with an error")
                    }

                    if testCase.field != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
                        t.Fatalf("expected the refusal to name %s, got %v", testCase.field, recoveredErr)
                    }
                }()

                registerBackendService(registrar, testCase.config)
            }()

            if 0 != len(registrar.names) {
                t.Fatalf("expected nothing registered, got %v", registrar.names)
            }
        })
    }
}

/* registerConnectionOver registers the connection service the rueidis module would, over a client the test holds */
func registerConnectionOver(serviceContainer containercontract.Container, client rueidis.Client) {
    serviceContainer.MustRegister(
        melodyrueidis.ServiceConnection,
        func(resolver containercontract.Resolver) (*melodyrueidis.Connection, error) {
            return melodyrueidis.NewConnection(client, "melody"), nil
        },
        containercontract.RegisterOptions{},
    )
}

func TestRegisterBackendService_AnApplicationWithNoConnectionIsRefusedAtTheResolve(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    registerBackendService(&containerRegistrar{target: serviceContainer}, BackendConfig{})

    if _, resolveErr := container.FromResolver[cachecontract.Backend](t.Context(), serviceContainer, melodycache.ServiceCacheBackend); nil == resolveErr {
        t.Fatal("expected a backend with no connection service refused")
    }
}

func (instance *containerRegistrar) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    return nil
}
