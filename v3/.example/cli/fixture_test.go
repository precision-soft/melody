package cli

import (
    "context"
    "errors"
    "io"
    "sync"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/.example/event"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const testUserServiceName = "service.test.user"

/* commandFixture builds the collaborators a command reaches through its lazy handle, out of the same doors
   the composition root uses rather than doubles: a handleless catalog storage falls back to the in-memory
   repository, which is seeded, and the cache is the manager the application registers, with the example's
   own GOB serializer. The serializer matters — a JSON one hands a map back where the service asserts
   *entity.User, and every door would fail on the double rather than on the code. */
type commandFixture struct {
    container      melodycontainercontract.Container
    runtime        melodyruntimecontract.Runtime
    userService    *service.UserService
    userRepository repository.UserRepository
}

func newCommandFixture(t *testing.T) *commandFixture {
    t.Helper()

    return newCommandFixtureWithDispatcher(t, nil)
}

/* newCommandFixtureWithDispatcher is the fixture with the dispatcher the user service dispatches through
   decorated — a dispatcher that refuses is how a command meets listeners whose backend is gone, after the write
   they follow has landed. */
func newCommandFixtureWithDispatcher(t *testing.T, dispatcherOf func(melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher) *commandFixture {
    t.Helper()

    storage := persistence.NewCatalogStorage(nil).WithAccountSeed()

    userRepository, repositoryErr := repository.NewUserRepository(storage)
    if nil != repositoryErr {
        t.Fatalf("build the user repository: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()

    cacheBackend := melodycache.NewInMemoryBackend(128, time.Minute, clockInstance)
    cacheInstance := melodycache.NewManagerOwningBackend(
        cacheBackend,
        examplecache.NewGobSerializer(),
    )

    /* the dispatcher carries the invalidation the application's subscriber performs on an update — the
       three entries an account is served from — so a second run of a command reads the directory, not the
       memo the first run planted before it wrote; the journal half of that subscriber is not wired, these
       tests read the directory and not the journal */
    dispatcher := melodyevent.NewEventDispatcher(clockInstance)
    dispatcher.AddListener(
        event.UserUpdatedEventName,
        func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
            updatedEvent, isUpdated := eventValue.Payload().(*event.UserUpdatedEvent)
            if false == isUpdated || nil == updatedEvent {
                return nil
            }

            for _, key := range []string{
                service.CacheKeyUserById(updatedEvent.User().Id),
                service.CacheKeyUserByUsername(updatedEvent.User().Username),
                service.CacheKeyUserList,
            } {
                if deleteErr := cacheInstance.Delete(key); nil != deleteErr {
                    return deleteErr
                }
            }

            return nil
        },
        0,
    )

    var dispatching melodyeventcontract.EventDispatcher = dispatcher
    if nil != dispatcherOf {
        dispatching = dispatcherOf(dispatching)
    }

    userService := service.NewUserService(
        userRepository,
        cacheInstance,
        dispatching,
    )

    containerInstance := melodycontainer.NewContainer()

    /* the backend is registered under the framework's own name, the way the composition root registers it: the scope helper of the writing commands reads the type of the backend there, and without the registration it would answer "this process's own" through its resolution-failure branch, a wiring the application never produces and one under which the shared-cache sentence could not be tested */
    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return cacheBackend, nil
        },
    )

    /* the event dispatcher resolves the logger for every dispatch, so a container without one turns the
       first event a command causes into a panic rather than into whatever the command was being tested for.
       It is the nop logger because these tests read the directory, not the journal. */
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    melodycontainer.MustRegister(
        containerInstance,
        testUserServiceName,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return userService, nil
        },
    )

    return &commandFixture{
        container:      containerInstance,
        runtime:        melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance),
        userService:    userService,
        userRepository: userRepository,
    }
}

func (instance *commandFixture) lazyUserService() *melodycontainer.LazyService[*service.UserService] {
    return melodycontainer.Lazy[*service.UserService](instance.container, testUserServiceName)
}

/* newFlagContext is the grant arm: the role as the flag the command requires, the user as its one argument. Its writer is io.Discard, because what a command PRINTS is not what these tests read — they read what the directory holds afterwards. */
func newFlagContext(role string, user string) *melodyclicontract.StaticContext {
    return newFlagContextWithWriter(role, user, nil)
}

/* newFlagContextWithWriter is the grant arm for a test that reads what the command printed */
func newFlagContextWithWriter(role string, user string, writer io.Writer) *melodyclicontract.StaticContext {
    return &melodyclicontract.StaticContext{
        StringValues:   map[string]string{"role": role},
        SetFlagNames:   []string{"role"},
        ArgumentValues: []string{user},
        WriterValue:    writer,
    }
}

/* newBoolFlagContext is the arm the reset command needs: its only flag is a bool, and what a test of that
   command reads is what the command wrote, so this one carries a writer of its own. */
func newBoolFlagContext(flagName string, value bool, writer io.Writer) *melodyclicontract.StaticContext {
    return &melodyclicontract.StaticContext{
        BoolValues:   map[string]bool{flagName: value},
        SetFlagNames: []string{flagName},
        WriterValue:  writer,
    }
}

/* newStringFlagContext gives the string flags a run would parse, each reported as set, and the writer the command prints to */
func newStringFlagContext(stringValues map[string]string, writer io.Writer) *melodyclicontract.StaticContext {
    setFlagNames := make([]string, 0, len(stringValues))
    for flagName := range stringValues {
        setFlagNames = append(setFlagNames, flagName)
    }

    return &melodyclicontract.StaticContext{
        StringValues: stringValues,
        SetFlagNames: setFlagNames,
        WriterValue:  writer,
    }
}

/* refusingWriter refuses every write, the shape of a closed pipe the operator's shell stopped reading */
type refusingWriter struct {
    refusal error
}

func (instance *refusingWriter) Write(payload []byte) (int, error) {
    return 0, instance.refusal
}

/* refusingDispatcher refuses every dispatch, the way a listener whose backend is gone would; the write it
   follows has already landed. */
type refusingDispatcher struct {
    melodyeventcontract.EventDispatcher
}

func (instance *refusingDispatcher) DispatchName(runtimeInstance melodyruntimecontract.Runtime, eventName string, payload any) (melodyeventcontract.Event, error) {
    return nil, errors.New("redis: connection refused")
}

/* clearCountingCache is the cache the reset clears: it counts the clears and keeps the order they came in relative to the recorded statements, so the pin can say the clear came AFTER the reseed rather than merely that it happened. */
type clearCountingCache struct {
    melodycachecontract.Cache

    mutex      sync.Mutex
    clearCount int
    onClear    func()
    refusal    error
}

func (instance *clearCountingCache) Clear() error {
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

    return instance.Cache.Clear()
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
) (melodyruntimecontract.Runtime, *clearCountingCache) {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return archiveStorage, nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            return storage, nil
        },
    )

    repository.RegisterSeeders(serviceContainer)

    backend := melodycache.NewInMemoryBackend(0, 0, melodyclock.NewSystemClock())
    cacheInstance := &clearCountingCache{Cache: melodycache.NewManagerOwningBackend(backend, melodycache.NewJsonSerializer())}

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return backend, nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
    )

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), cacheInstance
}
