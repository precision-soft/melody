package cli

import (
    "context"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* the command announced a grant it never made. It looked the account up, printed "granted role ... to
   user ...", and returned — for an account it had just been told did not exist as readily as for one it
   had found, exiting with success in both cases. Measured on the running stack before the repair, the
   directory answered the same roles after the command as before it.

   The assertion is on what the DIRECTORY holds afterwards, not on the line the command prints: printing
   the sentence is precisely what it used to do. */
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

    /* the role is ADDED, never substituted: the update door takes the whole set, so a command that passed
       the one role would strip every other the account holds. */
    for _, held := range before {
        if false == holdsRole(after, held) {
            t.Fatalf("expected the granted role to be added to %v, but the account now holds %v", before, after)
        }
    }
}

/* a second run must be a no-op rather than a second entry in the comma-joined column and a second line in
   the audit trail. */
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

/* the roles column is one comma-joined value, so a role carrying a comma comes back as several on the next
   read — an account granted "ROLE_X,ROLE_ADMIN" reads back holding ROLE_ADMIN. Both admin doors refuse it;
   this is the only other door that writes roles, and it refuses before it resolves anything. */
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

/* the roles are read from the REPOSITORY, not through the service. The service memoizes what it read, and
   the entry is cleared by the invalidation listener the application registers on the update event — which
   this fixture deliberately does not, since the subject here is the command and not the wiring. Read
   through the service, every assertion below would be about the memo: the first grant landed in the
   directory and the next read still answered the roles from before it. */
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
