package cli

import (
    "context"
    "io"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
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

    storage := persistence.NewCatalogStorage(nil)

    userRepository, repositoryErr := repository.NewUserRepository(storage)
    if nil != repositoryErr {
        t.Fatalf("build the user repository: %v", repositoryErr)
    }

    clockInstance := melodyclock.NewSystemClock()

    cacheInstance := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(128, time.Minute, clockInstance),
        examplecache.NewGobSerializer(),
    )

    userService := service.NewUserService(
        userRepository,
        cacheInstance,
        melodyevent.NewEventDispatcher(clockInstance),
    )

    containerInstance := melodycontainer.NewContainer()

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

/* flagContext answers the flags a command declared, and nothing else: the arms below turn on --role and
   --user alone. Writer answers io.Discard because what a command PRINTS is not what these tests read —
   they read what the directory holds afterwards. */
type flagContext struct {
    stringByName map[string]string
    boolByName   map[string]bool
    writer       io.Writer
}

func newFlagContext(role string, user string) *flagContext {
    return &flagContext{stringByName: map[string]string{"role": role, "user": user}}
}

/* newBoolFlagContext is the arm the reset command needs: its only flag is a bool, and what a test of that
   command reads is what the command wrote, so this one carries a writer of its own. */
func newBoolFlagContext(flagName string, value bool, writer io.Writer) *flagContext {
    return &flagContext{boolByName: map[string]bool{flagName: value}, writer: writer}
}

func (instance *flagContext) String(flagName string) string {
    return instance.stringByName[flagName]
}

func (instance *flagContext) Bool(flagName string) bool {
    return instance.boolByName[flagName]
}

func (instance *flagContext) Int(flagName string) int {
    return 0
}

func (instance *flagContext) StringSlice(flagName string) []string {
    return nil
}

func (instance *flagContext) IsSet(flagName string) bool {
    if _, exists := instance.stringByName[flagName]; true == exists {
        return true
    }

    _, exists := instance.boolByName[flagName]

    return exists
}

func (instance *flagContext) Arguments() []string {
    return nil
}

func (instance *flagContext) Writer() io.Writer {
    if nil == instance.writer {
        return io.Discard
    }

    return instance.writer
}

var _ melodyclicontract.Context = (*flagContext)(nil)
