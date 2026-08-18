package service

import (
    "context"
    "fmt"
    "strings"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/v2/.example/cache"
    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/event"
    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/security"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
    melodylogging "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

var userServiceFixtureTime = time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)

/* newUserServiceUnderTest builds the service over the real in-memory repository, whose seeds carry the same three credential pairs the e2e bands sign in with. */
func newUserServiceUnderTest(t *testing.T) *UserService {
    t.Helper()

    frozenClock := melodyclock.NewFrozenClock(userServiceFixtureTime)

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

/* keyGrammarCache refuses a key the backend grammar cannot carry, the way the redis backend does, and records every key it was asked for. It is what makes the guard above observable: over a map-backed cache an unsafe key is simply a map key and nothing distinguishes the guarded finder from the unguarded one. */
type keyGrammarCache struct {
    asked []string
}

func (instance *keyGrammarCache) refuse(key string) error {
    instance.asked = append(instance.asked, key)

    if true == strings.ContainsAny(key, " \n") {
        return fmt.Errorf("the cache backend refuses the key %q", key)
    }

    return nil
}

func (instance *keyGrammarCache) Get(key string) (any, bool, error) {
    if refusalErr := instance.refuse(key); nil != refusalErr {
        return nil, false, refusalErr
    }

    return nil, false, nil
}

func (instance *keyGrammarCache) Set(key string, value any, ttl time.Duration) error {
    return instance.refuse(key)
}

func (instance *keyGrammarCache) Delete(key string) error {
    return instance.refuse(key)
}

func (instance *keyGrammarCache) Has(key string) (bool, error) {
    if refusalErr := instance.refuse(key); nil != refusalErr {
        return false, refusalErr
    }

    return false, nil
}

func (instance *keyGrammarCache) Clear() error {
    return nil
}

func (instance *keyGrammarCache) Many(keys []string) (map[string]any, error) {
    return map[string]any{}, nil
}

func (instance *keyGrammarCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    return nil
}

func (instance *keyGrammarCache) DeleteMultiple(keys []string) error {
    return nil
}

func (instance *keyGrammarCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *keyGrammarCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *keyGrammarCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*keyGrammarCache)(nil)

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

/* the door folds the username through the one definition the cache keys and the invalidation listeners read, so the spelling that authenticates is the spelling the entry is written under */
func TestAuthenticateByUsernameAndPasswordFoldsTheUsername(t *testing.T) {
    userService := newUserServiceUnderTest(t)

    for _, spelling := range []string{"  editor  ", "EDITOR", "Editor"} {
        _, authenticated, authenticationErr := userService.AuthenticateByUsernameAndPassword(spelling, "editor")
        if nil != authenticationErr {
            t.Fatalf("authenticate %q: %v", spelling, authenticationErr)
        }

        if false == authenticated {
            t.Fatalf("the spelling %q of a seeded username was refused", spelling)
        }
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

/* an identifier the cache-key grammar refuses names a row no write door admits; before the guard, the finder handed the spelling to the cache and the refusal surfaced as a 500 on the read of an id that simply does not exist.

The cache under the assertion REFUSES such a key, the way the redis backend does. An in-memory backend accepts anything a Go map accepts, so over one the guard has no observable effect at all and the assertion agrees with itself — the same test written over the in-memory manager passes whether or not the guard is there. */
func TestFindByIdAnswersAbsentForACacheUnsafeIdentifier(t *testing.T) {
    refusingCache := &keyGrammarCache{}

    userService := NewUserService(
        repository.NewInMemoryUserRepository(),
        refusingCache,
        melodyevent.NewEventDispatcher(melodyclock.NewFrozenClock(userServiceFixtureTime)),
    )

    user, found, findErr := userService.FindById("a b")
    if nil != findErr {
        t.Fatalf("expected the unsafe identifier answered as absent, got error %v", findErr)
    }

    if true == found || nil != user {
        t.Fatal("expected the unsafe identifier to answer no user")
    }

    if 0 != len(refusingCache.asked) {
        t.Fatalf("the unsafe identifier reached the cache as %v", refusingCache.asked)
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
    frozenClock := melodyclock.NewFrozenClock(userServiceFixtureTime)

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
