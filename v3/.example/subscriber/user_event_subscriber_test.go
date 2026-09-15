package subscriber

import (
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodycache "github.com/precision-soft/melody/v3/cache"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

func TestUserDeletionAttemptsEnrollmentCleanupAfterCacheFailure(t *testing.T) {
    database, recorder := newRecordingDatabase()
    defer database.Close()
    cacheFailure := errors.New("cache deletion refused")
    containerInstance := melodycontainer.NewContainer()
    containerInstance.MustRegister(melodycache.ServiceCache, func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
        return &failingDeleteCache{failure: cacheFailure}, nil
    })
    containerInstance.MustRegister(melodylogging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return melodylogging.NewNopLogger(), nil
    })
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
    dispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
    dispatcher.AddSubscriber(NewUserEventSubscriberWithEnrollmentStore(twofactor.NewStore(database)))
    _, dispatchErr := dispatcher.DispatchName(runtimeInstance, event.UserDeletedEventName, event.NewUserDeletedEvent("user-4", "dave"))
    if false == errors.Is(dispatchErr, cacheFailure) {
        t.Fatalf("cache failure lost: %v", dispatchErr)
    }
    statements := recorder.recordedQueries()
    if 1 != len(statements) || false == strings.Contains(statements[0], "user_identifier = 'user-4'") {
        t.Fatalf("enrollment cleanup skipped: %v", statements)
    }
}
