package service

import (
    "context"
    "testing"
    "time"

    examplecache "github.com/precision-soft/melody/.example/cache"
    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/security"
    melodycache "github.com/precision-soft/melody/cache"
    melodyclock "github.com/precision-soft/melody/clock"
    melodyevent "github.com/precision-soft/melody/event"
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
