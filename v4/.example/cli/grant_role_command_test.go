package cli

import (
    "bytes"
    "context"
    "errors"
    "slices"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/repository"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
)

func TestGrantRoleCommandGrantsTheRole(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    before := storedRoles(t, fixture, "user")
    if true == slices.Contains(before, entity.RoleEditor) {
        t.Fatalf("expected the seeded account not to hold the role already, it holds %v", before)
    }

    if runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != runErr {
        t.Fatalf("expected the grant to succeed, got %v", runErr)
    }

    after := storedRoles(t, fixture, "user")
    if false == slices.Contains(after, entity.RoleEditor) {
        t.Fatalf("expected the account to hold the granted role, it holds %v", after)
    }

    for _, held := range before {
        if false == slices.Contains(after, held) {
            t.Fatalf("expected the granted role to be added to %v, but the account now holds %v", before, after)
        }
    }
}

func TestGrantRoleCommandIsANoOpWhenTheRoleIsAlreadyHeld(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    if runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != runErr {
        t.Fatalf("expected the first grant to succeed, got %v", runErr)
    }

    granted := storedRoles(t, fixture, "user")
    if false == slices.Contains(granted, entity.RoleEditor) {
        t.Fatalf("expected the first grant to land before the second is judged, the account holds %v", granted)
    }

    if runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != runErr {
        t.Fatalf("expected a second grant of the same role to succeed quietly, got %v", runErr)
    }

    again := storedRoles(t, fixture, "user")
    if strings.Join(granted, ",") != strings.Join(again, ",") {
        t.Fatalf("expected the second run to change nothing, %v became %v", granted, again)
    }
}

func TestGrantRoleCommandRefusesAnAccountThatDoesNotExist(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleAdmin, "nobody-here"))
    if nil == runErr {
        t.Fatalf("expected the command to refuse an account it could not find")
    }

    if false == strings.Contains(runErr.Error(), "nobody-here") {
        t.Fatalf("expected the refusal to name the account, got %q", runErr.Error())
    }
}

func TestGrantRoleCommandRefusesARoleCarryingAComma(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    runErr := command.Run(fixture.runtime, newFlagContext("ROLE_X,"+entity.RoleAdmin, "user"))
    if nil == runErr {
        t.Fatalf("expected a role carrying a comma to be refused")
    }

    if false == strings.Contains(runErr.Error(), "commas") {
        t.Fatalf("expected the refusal to name the comma, got %q", runErr.Error())
    }

    if true == slices.Contains(storedRoles(t, fixture, "user"), entity.RoleAdmin) {
        t.Fatalf("expected the refused grant to write nothing, but the account now holds the administrator role")
    }
}

func storedRoles(t *testing.T, fixture *commandFixture, username string) []string {
    t.Helper()

    user, found, findErr := fixture.userRepository.FindByUsername(context.Background(), username)
    if nil != findErr {
        t.Fatalf("find %q: %v", username, findErr)
    }

    if false == found {
        t.Fatalf("expected the seeded account %q to be there", username)
    }

    return append([]string{}, user.Roles...)
}

func TestGrantRoleCommandSaysWhenTheCacheIsThisProcessOwn(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    captured := &bytes.Buffer{}

    runErr := command.Run(fixture.runtime, newFlagContextWithWriter(entity.RoleEditor, "user", captured))

    if nil != runErr {
        t.Fatalf("expected the grant to succeed, got %v", runErr)
    }

    if false == strings.Contains(captured.String(), processLocalCacheNotice) {
        t.Fatalf("expected the command to say the cache is this process's own, got %q", captured.String())
    }
}

func TestGrantRoleCommandTrimsTheRoleBeforeStoringIt(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    for range 2 {
        if runErr := command.Run(fixture.runtime, newFlagContext("  "+entity.RoleEditor+" ", "user")); nil != runErr {
            t.Fatalf("a padded role was refused: %v", runErr)
        }
    }

    stored := storedRoles(t, fixture, "user")
    if 2 != len(stored) || entity.RoleUser != stored[0] || entity.RoleEditor != stored[1] {
        t.Fatalf("a padded role granted twice stored %v, wanted [%s %s] once", stored, entity.RoleUser, entity.RoleEditor)
    }
}

func TestGrantRoleCommandRefusesABlankRole(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    before := storedRoles(t, fixture, "user")

    runErr := command.Run(fixture.runtime, newFlagContext("   ", "user"))
    if nil == runErr || false == strings.Contains(runErr.Error(), `role "" is not one this application knows`) {
        t.Fatalf("expected a blank role refused as a role the application does not know, got %v", runErr)
    }

    after := storedRoles(t, fixture, "user")
    if strings.Join(before, ",") != strings.Join(after, ",") {
        t.Fatalf("a blank role changed the account from %v to %v", before, after)
    }
}

func TestGrantRoleCommandRefusesARoleTheApplicationDoesNotKnow(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    before := storedRoles(t, fixture, "user")

    runErr := command.Run(fixture.runtime, newFlagContext("ROLE_ADMIM", "user"))
    if nil == runErr {
        t.Fatal("a role the application does not know was granted")
    }

    if false == strings.Contains(runErr.Error(), "ROLE_ADMIM") || false == strings.Contains(runErr.Error(), entity.RoleAdmin) {
        t.Fatalf("the refusal reads %q, wanted it to name the spelling and the vocabulary", runErr.Error())
    }

    after := storedRoles(t, fixture, "user")
    if strings.Join(before, ",") != strings.Join(after, ",") {
        t.Fatalf("a refused role changed the account from %v to %v", before, after)
    }
}

func TestGrantRoleCommandGrantsThroughTheRepositorysAtomicDoor(t *testing.T) {
    fixture := newCommandFixture(t)

    account, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")

    _, outcome, grantErr := fixture.userRepository.GrantRole(context.Background(), account.Id, entity.RoleEditor)
    if nil != grantErr || repository.GrantRoleGranted != outcome {
        t.Fatalf("the first grant answered %d, %v", outcome, grantErr)
    }

    command := NewGrantRoleCommand(fixture.lazyUserService())
    if runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != runErr {
        t.Fatalf("a grant of a role held since the command's read failed: %v", runErr)
    }

    stored := storedRoles(t, fixture, "user")
    if 2 != len(stored) {
        t.Fatalf("the role was appended a second time: %v", stored)
    }

    if _, _, grantErr := fixture.userRepository.GrantRole(context.Background(), account.Id, entity.RoleAdmin); nil != grantErr {
        t.Fatalf("the admin grant failed: %v", grantErr)
    }

    stored = storedRoles(t, fixture, "user")
    if 3 != len(stored) || entity.RoleAdmin != stored[2] {
        t.Fatalf("two grants on one account stored %v, wanted user, editor, admin", stored)
    }
}

func TestGrantRoleCommandGrantsARoleTheCachedReadWronglySaysIsHeld(t *testing.T) {
    fixture := newCommandFixture(t)
    userService := fixture.lazyUserService()

    account, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")
    if _, _, grantErr := fixture.userRepository.GrantRole(context.Background(), account.Id, entity.RoleEditor); nil != grantErr {
        t.Fatalf("seed the editor role: %v", grantErr)
    }
    if _, _, findErr := userService.Get(melodyruntime.NewRun(t.Context(), fixture.container.NewScope())).FindByUsername(exampleTestRuntime(), "user"); nil != findErr {
        t.Fatalf("warm the memo: %v", findErr)
    }

    if _, _, updateErr := fixture.userRepository.Update(context.Background(), account.Id, repository.UserChange{Roles: []string{entity.RoleUser}}, nil); nil != updateErr {
        t.Fatalf("remove the role in the directory: %v", updateErr)
    }

    if runErr := NewGrantRoleCommand(userService).Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != runErr {
        t.Fatalf("the grant over a stale memo failed: %v", runErr)
    }

    if stored := storedRoles(t, fixture, "user"); false == slices.Contains(stored, entity.RoleEditor) {
        t.Fatalf("the cached read's word was taken over the directory's: the account holds %v", stored)
    }
}

func TestGrantRoleCommandReportsTheRepositorysAlreadyHeld(t *testing.T) {
    fixture := newCommandFixture(t)

    account, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")
    if _, _, grantErr := fixture.userRepository.GrantRole(context.Background(), account.Id, entity.RoleEditor); nil != grantErr {
        t.Fatalf("seed the editor role: %v", grantErr)
    }

    captured := &bytes.Buffer{}

    runErr := NewGrantRoleCommand(fixture.lazyUserService()).Run(fixture.runtime, newFlagContextWithWriter(entity.RoleEditor, "user", captured))

    if nil != runErr {
        t.Fatalf("a grant of a held role failed: %v", runErr)
    }

    if false == strings.Contains(captured.String(), "already holds role") || true == strings.Contains(captured.String(), "granted role") {
        t.Fatalf("the repository's already-held answer was reported as %q", captured.String())
    }

    if stored := storedRoles(t, fixture, "user"); 2 != len(stored) {
        t.Fatalf("a held role was appended again: %v", stored)
    }
}

func TestGrantRoleCommandSaysTheRoleWasGrantedWhenTheListenersRefuse(t *testing.T) {
    fixture := newCommandFixtureWithDispatcher(t, func(dispatcher melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher {
        return &refusingDispatcher{EventDispatcher: dispatcher}
    })
    command := NewGrantRoleCommand(fixture.lazyUserService())

    captured := &bytes.Buffer{}

    runErr := command.Run(fixture.runtime, newFlagContextWithWriter(entity.RoleEditor, "user", captured))

    if nil == runErr || false == strings.Contains(runErr.Error(), `the role was granted, and the cache entries of user "user" could not be dropped`) {
        t.Fatalf("expected the exit to say the role was granted and the entries stand, got %v", runErr)
    }

    if false == strings.Contains(captured.String(), `granted role "ROLE_EDITOR" to user "user", but the listeners that drop the account's cache entries were not told`) {
        t.Fatalf("expected the output to say the role was granted and name the listeners, got %q", captured.String())
    }

    if after := storedRoles(t, fixture, "user"); false == slices.Contains(after, entity.RoleEditor) {
        t.Fatalf("expected the committed grant in the directory, it holds %v", after)
    }
}

func TestGrantRoleCommandRefusesAnythingButOneNamedUser(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    before := storedRoles(t, fixture, "user")

    for _, arguments := range [][]string{nil, {"user", "admin"}} {
        runErr := command.Run(fixture.runtime, parsedContextValues{
            StringValues:   map[string]string{"role": entity.RoleEditor},
            ArgumentValues: arguments,
        }.parse())
        if nil == runErr || false == strings.Contains(runErr.Error(), "name exactly one user") {
            t.Fatalf("expected %q refused for not naming exactly one user, got %v", arguments, runErr)
        }
    }

    after := storedRoles(t, fixture, "user")
    if strings.Join(before, ",") != strings.Join(after, ",") {
        t.Fatalf("a refused invocation changed the account from %v to %v", before, after)
    }
}

func TestGrantRoleCommandRequiresTheRoleFlagAndSpellsItShortAsR(t *testing.T) {
    flags := NewGrantRoleCommand(nil).Flags()
    if 1 != len(flags) {
        t.Fatalf("expected the role flag alone, got %d flags", len(flags))
    }

    roleFlag, isString := flags[0].(*melodyclicontract.StringFlag)
    if false == isString || "role" != roleFlag.Name || true != roleFlag.Required || 1 != len(roleFlag.Aliases) || "r" != roleFlag.Aliases[0] {
        t.Fatalf("expected --role required with the alias -r, got %+v", flags[0])
    }
}

func TestGrantRoleCommandReadsThroughItsContext(t *testing.T) {
    fixture := newInterruptedCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    if runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); false == errors.Is(runErr, context.Canceled) {
        t.Fatalf("expected the interrupted grant to end on its context, got %v", runErr)
    }
}
