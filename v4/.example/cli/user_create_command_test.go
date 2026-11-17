package cli

import (
    "context"
    "errors"
    "io"
    "slices"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/persistence"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
)

func TestUserCreateCommand_CreatesAnAccountThatSignsInWithThePasswordReadFromItsInput(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewUserCreateCommand(fixture.lazyUserService(), strings.NewReader("  a-first-admin-password  \nignored second line\n"))

    if runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleAdmin, "operator")); nil != runErr {
        t.Fatalf("expected the account created, got %v", runErr)
    }

    account, authenticated, authenticateErr := fixture.userService.AuthenticateByUsernameAndPassword(context.Background(), "operator", "a-first-admin-password")
    if nil != authenticateErr || false == authenticated || nil == account {
        t.Fatalf("expected the created account to sign in with the password read from the input, got %v %v %v", account, authenticated, authenticateErr)
    }

    if false == slices.Equal([]string{entity.RoleAdmin}, account.Roles) {
        t.Fatalf("expected the account to hold the role it was given, it holds %v", account.Roles)
    }
}

func TestUserCreateCommand_RefusesAPasswordOnTheCommandLine(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewUserCreateCommand(fixture.lazyUserService(), strings.NewReader("from-the-input\n"))

    runErr := command.Run(fixture.runtime, parsedContextValues{
        StringValues:   map[string]string{"role": entity.RoleAdmin},
        ArgumentValues: []string{"operator", "a-password-on-the-command-line"},
    }.parse())
    if nil == runErr || false == strings.Contains(runErr.Error(), "never from an argument") {
        t.Fatalf("expected a password argument refused, got %v", runErr)
    }

    if _, known, _ := fixture.userService.FindByUsername(context.Background(), "operator"); true == known {
        t.Fatal("expected no account written when the password was passed as an argument")
    }
}

func TestUserCreateCommand_RefusesAnEmptyPasswordAnUnknownRoleAndATakenName(t *testing.T) {
    fixture := newCommandFixture(t)

    for _, refusalCase := range []struct {
        input    string
        role     string
        username string
        refusal  string
    }{
        {"", entity.RoleAdmin, "operator", "the password is required"},
        {"password\n", "ROLE_OWNER", "operator", "is not one this application knows"},
        {"password\n", entity.RoleUser, "admin", "already exists"},
    } {
        runErr := NewUserCreateCommand(fixture.lazyUserService(), strings.NewReader(refusalCase.input)).Run(fixture.runtime, newFlagContext(refusalCase.role, refusalCase.username))
        if nil == runErr || false == strings.Contains(runErr.Error(), refusalCase.refusal) {
            t.Fatalf("%+v: expected %q, got %v", refusalCase, refusalCase.refusal, runErr)
        }
    }

    users, _ := fixture.userRepository.All(context.Background())
    if 3 != len(users) {
        t.Fatalf("expected the directory to keep its three accounts, got %d", len(users))
    }
}

func TestUserCreateCommand_RefusesWithoutTheCatalogueDatabase(t *testing.T) {
    fixture := newCommandFixtureOver(t, nil, persistence.NewCatalogStorage(nil))
    command := NewUserCreateCommand(fixture.lazyUserService(), strings.NewReader("a-first-admin-password\n"))

    runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleAdmin, "operator"))
    if nil == runErr || false == strings.Contains(runErr.Error(), "needs the catalogue database") {
        t.Fatalf("expected the refusal naming the database, got %v", runErr)
    }

    if _, found, _ := fixture.userRepository.FindByUsername(context.Background(), "operator"); true == found {
        t.Fatalf("a refused create wrote the account")
    }
}

func TestUserCreateCommand_SaysTheCacheIsItsOwnOnTheInProcessFallback(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewUserCreateCommand(fixture.lazyUserService(), strings.NewReader("a-first-admin-password\n"))
    captured := &strings.Builder{}

    if runErr := command.Run(fixture.runtime, newFlagContextWithWriter(entity.RoleAdmin, "operator", captured)); nil != runErr {
        t.Fatalf("expected the account created, got %v", runErr)
    }

    if false == strings.Contains(captured.String(), processLocalCacheNotice) {
        t.Fatalf("expected the notice, got %q", captured.String())
    }
}

func TestUserCreateCommand_GivesUpTheReadWhenTheRuntimeContextEnds(t *testing.T) {
    fixture := newCommandFixture(t)
    readEnd, writeEnd := io.Pipe()
    t.Cleanup(func() { _ = writeEnd.Close() })

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance := melodyruntime.New(ctx, fixture.container.NewScope(), fixture.container)
    resultChannel := make(chan error, 1)

    go func() {
        resultChannel <- NewUserCreateCommand(fixture.lazyUserService(), readEnd).Run(runtimeInstance, newFlagContext(entity.RoleAdmin, "operator"))
    }()

    select {
    case runErr := <-resultChannel:
        if false == errors.Is(runErr, context.Canceled) {
            t.Fatalf("expected the ended context answered, got %v", runErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the read given up once the runtime context ended")
    }
}
