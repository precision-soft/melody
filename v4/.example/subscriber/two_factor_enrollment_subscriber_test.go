package subscriber

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    "github.com/precision-soft/melody/v4/.example/event"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func deleteEnrollmentStatementsAfter(t *testing.T, eventName string, payload any) []string {
    t.Helper()

    database, recorder := newRecordingDatabase()
    subscriberInstance := NewTwoFactorEnrollmentSubscriber(fixedTwoFactorStore(twofactor.NewStore(database)))

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    listeners := subscriberInstance.SubscribedEvents()[event.EventUserDeleted]
    if 1 != len(listeners) {
        t.Fatalf("expected one listener on %s, got %d", event.EventUserDeleted, len(listeners))
    }

    if listenErr := listeners[0].Listener()(runtimeInstance, melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock().Now())); nil != listenErr {
        t.Fatalf("expected the listener to answer nil, got %v", listenErr)
    }

    return recorder.recordedQueries()
}

func TestTwoFactorEnrollmentSubscriber_ADeletedAccountReleasesItsEnrollment(t *testing.T) {
    statements := deleteEnrollmentStatementsAfter(t, event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave"))

    if 2 != len(statements) || "DELETE FROM `melody_example_v4_two_factor_pending` WHERE (user_identifier = 'user-4')" != statements[0] {
        t.Fatalf("expected the pending enrollment, then the active one, deleted for the account, got %d: %v", len(statements), statements)
    }

    statement := statements[1]
    if false == strings.HasPrefix(statement, "DELETE FROM `melody_example_v4_two_factor`") || false == strings.Contains(statement, "WHERE (user_identifier = 'user-4')") {
        t.Fatalf("expected the enrollment of user-4 alone to be deleted, got %q", statement)
    }
}

func TestTwoFactorEnrollmentSubscriber_APayloadThatIsNotADeletionReleasesNothing(t *testing.T) {
    if statements := deleteEnrollmentStatementsAfter(t, event.EventUserDeleted, "user-4"); 0 != len(statements) {
        t.Fatalf("expected no statement for a payload that is not a deletion, got %v", statements)
    }

    var absent *event.UserDeletedEvent
    if statements := deleteEnrollmentStatementsAfter(t, event.EventUserDeleted, absent); 0 != len(statements) {
        t.Fatalf("expected no statement for a nil deletion, got %v", statements)
    }
}

type refusingCache struct {
    melodycachecontract.Cache
}

func (instance *refusingCache) Delete(runtimeInstance melodycontainercontract.Runtime, key string) error {
    return errors.New("redis: connection refused")
}

func TestTwoFactorEnrollmentSubscriber_ReleasesTheEnrollmentBeforeTheCacheListenerRuns(t *testing.T) {
    database, recorder := newRecordingDatabase()
    enrollmentSubscriber := NewTwoFactorEnrollmentSubscriber(fixedTwoFactorStore(twofactor.NewStore(database)))
    cacheSubscriber := NewUserEventSubscriber()

    releasePriority := enrollmentSubscriber.SubscribedEvents()[event.EventUserDeleted][0].Config().Priority
    cachePriority := cacheSubscriber.SubscribedEvents()[event.EventUserDeleted][0].Config().Priority
    if releasePriority <= cachePriority {
        t.Fatalf("expected the release to outrank the cache listener on %s, got %d against %d", event.EventUserDeleted, releasePriority, cachePriority)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: clockInstance})
    dispatcher.AddSubscriber(cacheSubscriber)
    dispatcher.AddSubscriber(enrollmentSubscriber)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    backend := melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{MaxItems: melodybound.Of(128), CleanupInterval: melodybound.Of(time.Minute), Clock: clockInstance})
    cacheInstance := &refusingCache{Cache: melodycache.NewManager(backend, examplecache.NewGobSerializer(), melodycache.ManagerConfig{OwnsBackend: true})}
    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
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
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave"))
    if nil == dispatchErr {
        t.Fatalf("expected the dispatch to fail on the refusing cache")
    }

    released := 0
    for _, statement := range recorder.recordedQueries() {
        if true == strings.HasPrefix(statement, "DELETE FROM `melody_example_v4_two_factor`") && true == strings.Contains(statement, "WHERE (user_identifier = 'user-4')") {
            released++
        }
    }
    if 1 != released {
        t.Fatalf("expected the enrollment of user-4 released ahead of the cache listener that failed, got %d releases in %v", released, recorder.recordedQueries())
    }
}

func TestTwoFactorEnrollmentSubscriber_AStoreThatCannotBeResolvedIsJournaledAndTheDeletionGoesOn(t *testing.T) {
    refusal := errors.New("the catalogue database refused the migration")
    asked := 0

    subscriberInstance := NewTwoFactorEnrollmentSubscriber(func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
        asked = asked + 1

        return nil, refusal
    })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    listener := subscriberInstance.SubscribedEvents()[event.EventUserDeleted][0].Listener()
    listenErr := listener(runtimeInstance, melodyevent.NewEvent(event.EventUserDeleted, event.NewUserDeletedEvent("user-4", "dave"), melodyclock.NewSystemClock().Now()))
    if nil != listenErr {
        t.Fatalf("expected the deletion to go on past a store that cannot be resolved, got %v", listenErr)
    }

    if 1 != asked {
        t.Fatalf("expected the store asked once for the deletion, got %d asks", asked)
    }
}
