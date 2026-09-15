package cli

import (
    "bytes"
    "context"
    "io"
    "os"
    "strings"
    "testing"
    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestGrantRoleCommandGrantsTheRole(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    before := storedRoles(t, fixture, "user")
    if true == holdsRole(before, entity.RoleEditor) {
        t.Fatalf("expected the seeded account not to hold the role already, it holds %v", before)
    }

    if runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != runErr {
        t.Fatalf("expected the grant to succeed, got %v", runErr)
    }

    after := storedRoles(t, fixture, "user")
    if false == holdsRole(after, entity.RoleEditor) {
        t.Fatalf("expected the account to hold the granted role, it holds %v", after)
    }

    for _, held := range before {
        if false == holdsRole(after, held) {
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
    if false == holdsRole(granted, entity.RoleEditor) {
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

    if true == holdsRole(storedRoles(t, fixture, "user"), entity.RoleAdmin) {
        t.Fatalf("expected the refused grant to write nothing, but the account now holds the administrator role")
    }
}

func TestGrantRoleCommandSaysWhenTheCacheIsThisProcessOwn(t *testing.T) {
    fixture := newCommandFixture(t)
    command := NewGrantRoleCommand(fixture.lazyUserService())

    previousStdout := os.Stdout
    reader, writer, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("open the capture pipe: %v", pipeErr)
    }
    os.Stdout = writer

    runErr := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user"))

    _ = writer.Close()
    os.Stdout = previousStdout

    captured := &bytes.Buffer{}
    _, _ = io.Copy(captured, reader)
    _ = reader.Close()

    if nil != runErr {
        t.Fatalf("expected the grant to succeed, got %v", runErr)
    }

    if false == strings.Contains(captured.String(), processLocalCacheNotice) {
        t.Fatalf("expected the command to say the cache is this process's own, got %q", captured.String())
    }
}

func TestGrantRoleNormalizesAndRejectsUnknownRoles(t *testing.T) {
    for _, role := range []string{" ROLE_EDITOR ", " ", "ROLE_ADMIM"} {
        t.Run(role, func(t *testing.T) {
            fixture := newCommandFixture(t)
            err := NewGrantRoleCommand(fixture.lazyUserService()).Run(fixture.runtime, newFlagContext(role, "user"))
            if " ROLE_EDITOR " == role {
                if nil != err || false == holdsRole(storedRoles(t, fixture, "user"), entity.RoleEditor) { t.Fatalf("trimmed valid grant failed: %v", err) }
            } else if nil == err { t.Fatalf("invalid role %q accepted", role) }
        })
    }
}

func TestGrantRolePreservesChangesAfterCachedLookup(t *testing.T) {
    fixture := newCommandFixture(t)
    _, _, err := fixture.userService.FindByUsername("user")
    if nil != err { t.Fatal(err) }
    account, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")
    changed := *account
    changed.Password = "replacement-password-hash"
    changed.Roles = append(append([]string{}, account.Roles...), entity.RoleAdmin)
    if _, err := fixture.userRepository.Update(context.Background(), &changed); nil != err { t.Fatal(err) }
    command := NewGrantRoleCommand(fixture.lazyUserService())
    if err := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != err { t.Fatal(err) }
    after, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")
    if changed.Password != after.Password || false == holdsRole(after.Roles, entity.RoleAdmin) || false == holdsRole(after.Roles, entity.RoleEditor) {
        t.Fatalf("grant overwrote a newer account: %+v", after)
    }
    previous := after
    if err := command.Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != err { t.Fatal(err) }
    after, _, _ = fixture.userRepository.FindByUsername(context.Background(), "user")
    if previous != after { t.Fatal("repeated grant wrote a new in-memory record") }
}
