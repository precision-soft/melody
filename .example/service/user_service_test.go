package service

import (
    "context"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/.example/cache"
    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/event"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/security"
    melodycache "github.com/precision-soft/melody/cache"
    melodyclock "github.com/precision-soft/melody/clock"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyevent "github.com/precision-soft/melody/event"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodylogging "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

/* newUserServiceUnderTest builds the service over the real in-memory repository, whose seeds carry the same three credential pairs the e2e bands sign in with. */
func newUserServiceUnderTest(t *testing.T) *UserService {
    t.Helper()

    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC))

    cacheManager := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(0, time.Minute, frozenClock),
        examplecache.NewGobSerializer(),
    )
    t.Cleanup(func() {
        _ = cacheManager.Close()
    })

    return NewUserService(
        repository.NewInMemoryUserRepository(),
        cacheManager,
        melodyevent.NewEventDispatcher(frozenClock),
    )
}

func TestAuthenticateByUsernameAndPasswordAcceptsTheSeededCredentials(t *testing.T) {
    userService := newUserServiceUnderTest(t)

    user, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword("editor", "editor")
    if nil != authenticationErr {
        t.Fatalf("authenticate the seeded editor: %v", authenticationErr)
    }

    if false == authenticated {
        t.Fatalf("the seeded editor was refused")
    }

    if "user-2" != user.Id {
        t.Fatalf("unexpected user: %q", user.Id)
    }
}

func TestAuthenticateByUsernameAndPasswordTrimsTheUsername(t *testing.T) {
    userService := newUserServiceUnderTest(t)

    _, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword("  editor  ", "editor")
    if nil != authenticationErr {
        t.Fatalf("authenticate with a padded username: %v", authenticationErr)
    }

    if false == authenticated {
        t.Fatalf("a padded spelling of a seeded username was refused")
    }
}

/* Every refusal below is the same quiet (nil, false, nil), so each guard is driven on its own: a caller cannot tell WHICH guard refused, and the point of each case is that its input never reaches the comparison. */
func TestAuthenticateByUsernameAndPasswordRefusesQuietly(t *testing.T) {
    for name, credentials := range map[string][2]string{
        "an empty username":       {"", "editor"},
        "a blank username":        {"   ", "editor"},
        "an empty password":       {"editor", ""},
        "a wrong password":        {"editor", "editor-but-wrong"},
        "an unknown username":     {"nobody", "editor"},
        "another user's password": {"editor", "admin"},
    } {
        t.Run(name, func(t *testing.T) {
            userService := newUserServiceUnderTest(t)

            user, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword(credentials[0], credentials[1])
            if nil != authenticationErr {
                t.Fatalf("the refusal carried an error: %v", authenticationErr)
            }

            if true == authenticated || nil != user {
                t.Fatalf("the credentials were accepted")
            }
        })
    }
}

func TestAuthenticateByUsernameAndPasswordRefusesAUserWithoutRoles(t *testing.T) {
    userService := newUserServiceUnderTest(t)

    createErr := userService.userRepository.Create(
        context.Background(),
        entity.NewUser("user-9", "bare", security.MustHashPassword("bare"), nil),
    )
    if nil != createErr {
        t.Fatalf("create the roleless user: %v", createErr)
    }

    _, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword("bare", "bare")
    if nil == authenticationErr {
        t.Fatalf("a user without roles authenticated quietly")
    }

    if true == authenticated {
        t.Fatalf("a user without roles was authenticated")
    }
}

/* an identifier the cache-key grammar refuses names a row no write door admits; before the guard, the finder handed the spelling to the cache and the refusal surfaced as a 500 on the read of an id that simply does not exist */
func TestFindByIdAnswersAbsentForACacheUnsafeIdentifier(t *testing.T) {
    userService := newUserServiceUnderTest(t)

    user, found, findErr := userService.FindById("a b")
    if nil != findErr {
        t.Fatalf("expected the unsafe identifier answered as absent, got error %v", findErr)
    }

    if true == found || nil != user {
        t.Fatal("expected the unsafe identifier to answer no user")
    }
}

func TestAuthenticateRefusesACacheUnsafeUsernameQuietly(t *testing.T) {
    userService := newUserServiceUnderTest(t)

    user, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword("john doe", "whatever")
    if nil != authenticationErr {
        t.Fatalf("expected the quiet refusal, got error %v", authenticationErr)
    }

    if true == authenticated || nil != user {
        t.Fatal("expected the unsafe username to authenticate nobody")
    }
}

/* the previous spelling travels in the event precisely so the listener can drop the cache entry a rename leaves behind — the updated entity no longer knows it */
func TestUpdateCarriesThePreviousUsernameOnTheEvent(t *testing.T) {
    frozenClock := melodyclock.NewFrozenClock(time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC))

    cacheManager := melodycache.NewManagerOwningBackend(
        melodycache.NewInMemoryBackend(0, time.Minute, frozenClock),
        examplecache.NewGobSerializer(),
    )
    t.Cleanup(func() {
        _ = cacheManager.Close()
    })

    dispatcher := melodyevent.NewEventDispatcher(frozenClock)
    userService := NewUserService(repository.NewInMemoryUserRepository(), cacheManager, dispatcher)

    var captured *event.UserUpdatedEvent
    dispatcher.AddListener(
        event.UserUpdatedEventName,
        func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
            if payload, ok := eventValue.Payload().(*event.UserUpdatedEvent); true == ok {
                captured = payload
            }

            return nil
        },
        0,
    )

    users, listErr := userService.List()
    if nil != listErr || 0 == len(users) {
        t.Fatalf("expected the seeded users, got %d and %v", len(users), listErr)
    }

    target := users[0]

    /* the dispatcher resolves the journal logger from the runtime, so the test container carries a silent one */
    serviceContainer := melodycontainer.NewContainer()
    melodycontainer.MustRegister[loggingcontract.Logger](
        serviceContainer,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (loggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    runtimeInstance := melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    _, updated, updateErr := userService.Update(runtimeInstance, target.Id, "renamed-user", target.Password, target.Roles)
    if nil != updateErr || false == updated {
        t.Fatalf("expected the update to land, got updated=%v err=%v", updated, updateErr)
    }

    if nil == captured {
        t.Fatal("the updated event never reached the listener")
    }

    if target.Username != captured.PreviousUsername() {
        t.Fatalf("expected the event to carry the previous username %q, got %q", target.Username, captured.PreviousUsername())
    }

    if "renamed-user" != captured.User().Username {
        t.Fatalf("expected the event to carry the new username, got %q", captured.User().Username)
    }
}
