package cli

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "sort"
    "sync"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/service"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
    melodylockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

const testUserServiceName = "service.test.user"

type commandFixture struct {
    container      melodycontainercontract.Container
    runtime        melodycontainercontract.Runtime
    userService    *service.UserService
    userRepository repository.UserRepository
}

func newCommandFixture(t *testing.T) *commandFixture {
    t.Helper()

    return newCommandFixtureWithDispatcher(t, nil)
}

func newCommandFixtureWithDispatcher(t *testing.T, dispatcherOf func(melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher) *commandFixture {
    t.Helper()

    return newCommandFixtureOver(t, dispatcherOf, persistence.NewCatalogStorage(bun.NewDB(sql.OpenDB(&undialledConnector{}), mysqldialect.New())))
}

func newCommandFixtureOver(t *testing.T, dispatcherOf func(melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher, catalogue *persistence.CatalogStorage) *commandFixture {
    t.Helper()

    return newCommandFixtureOverUsers(t, dispatcherOf, catalogue, func(userRepository repository.UserRepository) repository.UserRepository {
        return userRepository
    })
}

func newCommandFixtureOverUsers(t *testing.T, dispatcherOf func(melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher, catalogue *persistence.CatalogStorage, wrapUsers func(repository.UserRepository) repository.UserRepository) *commandFixture {
    t.Helper()

    storage := persistence.NewCatalogStorage(nil).WithAccountSeed()

    userRepository, repositoryErr := repository.NewUserRepository(storage, &persistence.CatalogSchema{})
    if nil != repositoryErr {
        t.Fatalf("build the user repository: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()

    cacheBackend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance})
    cacheInstance := melodycache.NewManager(
        cacheBackend,
        examplecache.NewGobSerializer(),
        melodycache.ManagerConfig{OwnsBackend: true},
    )

    dispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: clockInstance})
    dispatcher.AddListener(
        event.EventUserUpdated,
        func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
            updatedEvent, isUpdated := eventValue.Payload().(*event.UserUpdatedEvent)
            if false == isUpdated || nil == updatedEvent {
                return nil
            }

            for _, key := range []string{
                service.CacheKeyUserById(updatedEvent.User().Id),
                service.CacheKeyUserByUsername(updatedEvent.User().Username),
                service.CacheKeyUserList,
            } {
                if deleteErr := cacheInstance.Delete(exampleTestRuntime(), key); nil != deleteErr {
                    return deleteErr
                }
            }

            return nil
        },
        melodyeventcontract.ListenerConfig{},
    )

    var dispatching melodyeventcontract.EventDispatcher = dispatcher
    if nil != dispatcherOf {
        dispatching = dispatcherOf(dispatching)
    }

    userService := service.NewUserService(
        wrapUsers(userRepository),
        cacheInstance,
        dispatching,
    )

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return cacheBackend, nil
        },
        melodycontainer.RegisterOptions{},
    )

    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    melodycontainer.MustRegister(
        containerInstance,
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            return catalogue, nil
        },
        melodycontainer.RegisterOptions{},
    )

    melodycontainer.MustRegister(
        containerInstance,
        testUserServiceName,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return userService, nil
        },
        melodycontainer.RegisterOptions{},
    )

    return &commandFixture{
        container:      containerInstance,
        runtime:        melodyruntime.NewRun(context.Background(), containerInstance.NewScope()),
        userService:    userService,
        userRepository: userRepository,
    }
}

func (instance *commandFixture) lazyUserService() *melodycontainer.LazyService[*service.UserService] {
    return melodycontainer.Lazy[*service.UserService](instance.container, testUserServiceName)
}

func newFlagContext(role string, user string) *melodyclicontract.CommandContext {
    return newFlagContextWithWriter(role, user, nil)
}

func newFlagContextWithWriter(role string, user string, writer io.Writer) *melodyclicontract.CommandContext {
    return parsedContextValues{
        StringValues:   map[string]string{"role": role},
        ArgumentValues: []string{user},
        WriterValue:    writer,
    }.parse()
}

func newBoolFlagContext(flagName string, value bool, writer io.Writer) *melodyclicontract.CommandContext {
    return parsedContextValues{
        BoolValues:  map[string]bool{flagName: value},
        WriterValue: writer,
    }.parse()
}

func newStringFlagContext(stringValues map[string]string, writer io.Writer) *melodyclicontract.CommandContext {
    return parsedContextValues{
        StringValues: stringValues,
        WriterValue:  writer,
    }.parse()
}

type parsedContextValues struct {
    StringValues      map[string]string
    BoolValues        map[string]bool
    IntValues         map[string]int
    StringSliceValues map[string][]string
    ArgumentValues    []string
    WriterValue       io.Writer
}

func (instance parsedContextValues) parse() *melodyclicontract.CommandContext {
    flags := make([]melodyclicontract.Flag, 0)
    arguments := []string{"test"}

    for _, flagName := range sortedKeys(instance.StringValues) {
        flags = append(flags, &melodyclicontract.StringFlag{Name: flagName})
        arguments = append(arguments, "--"+flagName, instance.StringValues[flagName])
    }

    for _, flagName := range sortedKeys(instance.BoolValues) {
        flags = append(flags, &melodyclicontract.BoolFlag{Name: flagName})
        arguments = append(arguments, fmt.Sprintf("--%s=%t", flagName, instance.BoolValues[flagName]))
    }

    for _, flagName := range sortedKeys(instance.IntValues) {
        flags = append(flags, &melodyclicontract.IntFlag{Name: flagName})
        arguments = append(arguments, fmt.Sprintf("--%s=%d", flagName, instance.IntValues[flagName]))
    }

    for _, flagName := range sortedKeys(instance.StringSliceValues) {
        flags = append(flags, &melodyclicontract.StringSliceFlag{Name: flagName})
        for _, value := range instance.StringSliceValues[flagName] {
            arguments = append(arguments, "--"+flagName, value)
        }
    }

    arguments = append(arguments, "--")
    arguments = append(arguments, instance.ArgumentValues...)

    writer := instance.WriterValue
    if nil == writer {
        writer = io.Discard
    }

    commandContext := &melodyclicontract.CommandContext{
        Name:            "test",
        Flags:           flags,
        Writer:          writer,
        ErrWriter:       writer,
        HideHelpCommand: true,
        Action: func(ctx context.Context, actionCommand *melodyclicontract.CommandContext) error {
            return nil
        },
    }

    if runErr := commandContext.Run(context.Background(), arguments); nil != runErr {
        panic(runErr)
    }

    return commandContext
}

func sortedKeys[V any](values map[string]V) []string {
    keys := make([]string, 0, len(values))
    for key := range values {
        keys = append(keys, key)
    }

    sort.Strings(keys)

    return keys
}

type refusingWriter struct {
    refusal error
}

func (instance *refusingWriter) Write(payload []byte) (int, error) {
    return 0, instance.refusal
}

type refusingDispatcher struct {
    melodyeventcontract.EventDispatcher
}

func (instance *refusingDispatcher) DispatchName(runtimeInstance melodycontainercontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    return nil, errors.New("redis: connection refused")
}

type clearCountingCache struct {
    melodycachecontract.Cache

    mutex      sync.Mutex
    clearCount int
    onClear    func()
    refusal    error
}

func (instance *clearCountingCache) Clear(runtimeInstance melodycontainercontract.Runtime) error {
    instance.mutex.Lock()
    instance.clearCount = instance.clearCount + 1
    onClear := instance.onClear
    refusal := instance.refusal
    instance.mutex.Unlock()

    if nil != onClear {
        onClear()
    }

    if nil != refusal {
        return refusal
    }

    return instance.Cache.Clear(runtimeInstance)
}

func (instance *clearCountingCache) clears() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.clearCount
}

func newResetRuntimeWithArchive(
    t *testing.T,
    storage *persistence.CatalogStorage,
    archiveStorage *persistence.ArchiveStorage,
) (melodycontainercontract.Runtime, *clearCountingCache) {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return archiveStorage, nil
        },
        melodycontainer.RegisterOptions{},
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            return storage, nil
        },
        melodycontainer.RegisterOptions{},
    )

    repository.RegisterSeeders(serviceContainer)

    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{Clock: melodyclock.NewSystemClock()})
    cacheInstance := &clearCountingCache{Cache: melodycache.NewManager(backend, melodycache.NewJsonSerializer(), melodycache.ManagerConfig{OwnsBackend: true})}

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return backend, nil
        },
        melodycontainer.RegisterOptions{},
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
        melodycontainer.RegisterOptions{},
    )

    return melodyruntime.NewRun(context.Background(), serviceContainer.NewScope()), cacheInstance
}

func mustProductRepository(t *testing.T) repository.ProductRepository {
    t.Helper()

    productRepository, productRepositoryErr := repository.NewProductRepository(persistence.NewCatalogStorage(nil), &persistence.CatalogSchema{})
    if nil != productRepositoryErr {
        t.Fatalf("build the product repository: %v", productRepositoryErr)
    }

    return productRepository
}

type undialledConnector struct{}

func (instance *undialledConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("the test database is never dialled")
}

func (instance *undialledConnector) Driver() driver.Driver {
    return nil
}

func waitForTheCallerContext(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(5 * time.Second):
        return nil
    }
}

func endedContext() context.Context {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    return ctx
}

type stalledUserRepository struct {
    repository.UserRepository
}

func (instance *stalledUserRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, waitForTheCallerContext(ctx)
}

func newInterruptedCommandFixture(t *testing.T) *commandFixture {
    t.Helper()

    fixture := newCommandFixtureOverUsers(t, nil, persistence.NewCatalogStorage(bun.NewDB(sql.OpenDB(&undialledConnector{}), mysqldialect.New())), func(userRepository repository.UserRepository) repository.UserRepository {
        return &stalledUserRepository{UserRepository: userRepository}
    })
    fixture.runtime = melodyruntime.NewRun(endedContext(), fixture.container.NewScope())

    return fixture
}

type stalledProductRepository struct {
    repository.ProductRepository
}

func (instance *stalledProductRepository) All(ctx context.Context) ([]*entity.Product, error) {
    return nil, waitForTheCallerContext(ctx)
}

type stalledCategoryRepository struct {
    repository.CategoryRepository
}

func (instance *stalledCategoryRepository) All(ctx context.Context) ([]*entity.Category, error) {
    return nil, waitForTheCallerContext(ctx)
}

type stalledCurrencyRepository struct {
    repository.CurrencyRepository
}

func (instance *stalledCurrencyRepository) All(ctx context.Context) ([]*entity.Currency, error) {
    return nil, waitForTheCallerContext(ctx)
}

func runtimeWithClock(t *testing.T, clockInstance melodyclockcontract.Clock) melodycontainercontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    melodycontainer.MustRegister(
        containerInstance,
        melodyclock.ServiceClock,
        func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
            return clockInstance, nil
        },
        melodycontainer.RegisterOptions{},
    )

    return melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
}

func exampleTestRuntime() melodycontainercontract.Runtime {
    return exampleTestRuntimeUnder(context.Background())
}

func exampleTestRuntimeUnder(ctx context.Context) melodycontainercontract.Runtime {
    return melodyruntime.NewRun(ctx, melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}

/* mustCreateLock answers the lock a test's CreateLock made, a refusal failing the test where it happened */
func mustCreateLock(lock melodylockcontract.Lock, createErr error) melodylockcontract.Lock {
    if nil != createErr {
        panic(createErr)
    }

    return lock
}
