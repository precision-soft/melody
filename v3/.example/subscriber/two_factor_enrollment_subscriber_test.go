package subscriber

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func deleteEnrollmentStatementsAfter(t *testing.T, eventName string, payload any) []string {
    t.Helper()

    database, recorder := newRecordingDatabase()
    subscriberInstance := NewTwoFactorEnrollmentSubscriber(fixedTwoFactorStore(twofactor.NewStore(database)))

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    listeners := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName]
    if 1 != len(listeners) {
        t.Fatalf("expected one listener on %s, got %d", event.UserDeletedEventName, len(listeners))
    }

    if listenErr := listeners[0].Listener()(runtimeInstance, melodyevent.NewEvent(eventName, payload, melodyclock.NewSystemClock())); nil != listenErr {
        t.Fatalf("expected the listener to answer nil, got %v", listenErr)
    }

    return recorder.recordedQueries()
}

/* the enrollment is keyed on the account identifier and the example mints identifiers as the highest suffix plus one, so a row that outlives its account would start the next holder of the identifier enrolled with the previous holder's secret and recovery codes: the deletion the account publishes releases the factor, keyed on the identifier the event carries and on nothing wider. */
func TestTwoFactorEnrollmentSubscriber_ADeletedAccountReleasesItsEnrollment(t *testing.T) {
    statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave"))

    if 1 != len(statements) {
        t.Fatalf("expected one statement for the deleted account, got %d: %v", len(statements), statements)
    }

    statement := statements[0]
    if false == strings.HasPrefix(statement, "DELETE FROM `melody_example_v3_two_factor`") || false == strings.Contains(statement, "WHERE (user_identifier = 'user-4')") {
        t.Fatalf("expected the enrollment of user-4 alone to be deleted, got %q", statement)
    }
}

/* a payload that is not a deletion — or none at all — releases nothing: the listener is registered on the deletion name and still reads the payload it is handed, the way every subscriber of the example does. */
func TestTwoFactorEnrollmentSubscriber_APayloadThatIsNotADeletionReleasesNothing(t *testing.T) {
    if statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, "user-4"); 0 != len(statements) {
        t.Fatalf("expected no statement for a payload that is not a deletion, got %v", statements)
    }

    var absent *event.UserDeletedEvent
    if statements := deleteEnrollmentStatementsAfter(t, event.UserDeletedEventName, absent); 0 != len(statements) {
        t.Fatalf("expected no statement for a nil deletion, got %v", statements)
    }
}

/* refusingCache refuses every delete, the way the cache listener meets its backend under a redis outage */
type refusingCache struct {
    melodycachecontract.Cache
}

func (instance *refusingCache) Delete(key string) error {
    return errors.New("redis: connection refused")
}

/* the dispatcher ends a dispatch at the first listener that fails, and the composition root registers the cache subscriber before this one, so at the cache listener's own priority a redis outage at the deletion would leave the enrollment for the next holder of the identifier. The release outranks the cache listener on the deletion event, read off the two real subscribers, and with the cache refusing its first delete the enrollment is still released: the dispatch fails on the cache, after the row is gone. */
func TestTwoFactorEnrollmentSubscriber_ReleasesTheEnrollmentBeforeTheCacheListenerRuns(t *testing.T) {
    database, recorder := newRecordingDatabase()
    enrollmentSubscriber := NewTwoFactorEnrollmentSubscriber(fixedTwoFactorStore(twofactor.NewStore(database)))
    cacheSubscriber := NewUserEventSubscriber()

    releasePriority := enrollmentSubscriber.SubscribedEvents()[event.UserDeletedEventName][0].Priority()
    cachePriority := cacheSubscriber.SubscribedEvents()[event.UserDeletedEventName][0].Priority()
    if releasePriority <= cachePriority {
        t.Fatalf("expected the release to outrank the cache listener on %s, got %d against %d", event.UserDeletedEventName, releasePriority, cachePriority)
    }

    clockInstance := melodyclock.NewSystemClock()
    dispatcher := melodyevent.NewEventDispatcher(clockInstance)
    dispatcher.AddSubscriber(cacheSubscriber)
    dispatcher.AddSubscriber(enrollmentSubscriber)

    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    backend := melodycache.NewInMemoryBackend(128, time.Minute, clockInstance)
    cacheInstance := &refusingCache{Cache: melodycache.NewManagerOwningBackend(backend, examplecache.NewGobSerializer())}
    melodycontainer.MustRegister(
        containerInstance,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
    )
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave"))
    if nil == dispatchErr {
        t.Fatalf("expected the dispatch to fail on the refusing cache")
    }

    released := 0
    for _, statement := range recorder.recordedQueries() {
        if true == strings.HasPrefix(statement, "DELETE FROM `melody_example_v3_two_factor`") && true == strings.Contains(statement, "WHERE (user_identifier = 'user-4')") {
            released++
        }
    }
    if 1 != released {
        t.Fatalf("expected the enrollment of user-4 released ahead of the cache listener that failed, got %d releases in %v", released, recorder.recordedQueries())
    }
}

/* the release resolves the store at each deletion, and a store that cannot be resolved fails the deletion rather than passing it with the enrollment standing: the listener hands the refusal back, so the dispatch, and the door that deleted the account, answers it. */
func TestTwoFactorEnrollmentSubscriber_AStoreThatCannotBeResolvedFailsTheDeletion(t *testing.T) {
    refusal := errors.New("the catalogue database refused the migration")
    asked := 0

    subscriberInstance := NewTwoFactorEnrollmentSubscriber(func(runtimeInstance melodyruntimecontract.Runtime) (*twofactor.Store, error) {
        asked = asked + 1

        return nil, refusal
    })

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    listener := subscriberInstance.SubscribedEvents()[event.UserDeletedEventName][0].Listener()
    listenErr := listener(runtimeInstance, melodyevent.NewEvent(event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave"), melodyclock.NewSystemClock()))
    if false == errors.Is(listenErr, refusal) {
        t.Fatalf("expected the deletion to fail with the store's refusal, got %v", listenErr)
    }

    if 1 != asked {
        t.Fatalf("expected the store asked once for the deletion, got %d asks", asked)
    }
}
