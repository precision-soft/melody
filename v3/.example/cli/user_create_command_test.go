package cli

import (
    "context"
    "slices"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
)

/* the account the command creates is one the directory holds and the password authenticates: a command that only printed would pass an assertion on its output */
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

/* the password is never an argument: a second argument is refused before anything is read or written */
func TestUserCreateCommand_RefusesAPasswordOnTheCommandLine(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewUserCreateCommand(fixture.lazyUserService(), strings.NewReader("from-the-input\n"))

    runErr := command.Run(fixture.runtime, &melodyclicontract.StaticContext{
        StringValues:   map[string]string{"role": entity.RoleAdmin},
        SetFlagNames:   []string{"role"},
        ArgumentValues: []string{"operator", "a-password-on-the-command-line"},
    })
    if nil == runErr || false == strings.Contains(runErr.Error(), "never from an argument") {
        t.Fatalf("expected a password argument refused, got %v", runErr)
    }

    if _, known, _ := fixture.userService.FindByUsername("operator"); true == known {
        t.Fatal("expected no account written when the password was passed as an argument")
    }
}

/* an empty input, an unknown role and a taken name are refused, and the directory keeps what it held */
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
