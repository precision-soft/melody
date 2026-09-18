package cli

import (
    "slices"
    "bytes"
    "context"
    "io"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
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

    /* the role is ADDED, never substituted: the update door takes the whole set, so a command that passed
       the one role would strip every other the account holds. */
    for _, held := range before {
        if false == slices.Contains(after, held) {
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

    if true == slices.Contains(storedRoles(t, fixture, "user"), entity.RoleAdmin) {
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

/* the grant's listeners drop the account's cache entries in the process that dispatched, and the fixture's cache is the in-process map: the command has to say so, because a session opened against a server on the same fallback keeps the roles it cached until that server restarts. Read off the process's standard output, which is where the command's own line goes. */
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

/* the flag is stored as the admin doors store a role — trimmed — so a re-run finds the role held instead of
   appending it again with its space */
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

    if runErr := command.Run(fixture.runtime, newFlagContext("   ", "user")); nil != runErr {
        t.Fatalf("a blank role errored instead of being answered as no role: %v", runErr)
    }

    after := storedRoles(t, fixture, "user")
    if strings.Join(before, ",") != strings.Join(after, ",") {
        t.Fatalf("a blank role changed the account from %v to %v", before, after)
    }
}

/* the voter compares a role's spelling exactly, so a spelling outside the application's vocabulary would be
   stored, reported granted and grant nothing */
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

/* the repository is the arbiter, not the read the command made: two grants of different roles on one
   account, each reading the account before either wrote, both land — a read, an append and a whole-set
   write used to let the last writer take the other's role with it */
func TestGrantRoleCommandGrantsThroughTheRepositorysAtomicDoor(t *testing.T) {
    fixture := newCommandFixture(t)

    account, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")

    _, outcome, grantErr := fixture.userRepository.GrantRole(context.Background(), account.Id, entity.RoleEditor)
    if nil != grantErr || repository.GrantRoleGranted != outcome {
        t.Fatalf("the first grant answered %d, %v", outcome, grantErr)
    }

    /* the command's read is the pre-grant account (the fixture's cache was never told), and the repository
       still answers the truth */
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

/* the other direction of the arbiter: the command's read says the role is held — a memo from before an
   admin door removed it — and the directory says it is not. The repository grants, because it is the only
   judge; a short-cut on the cached roles used to answer "already holds" and leave the row unchanged. */
func TestGrantRoleCommandGrantsARoleTheCachedReadWronglySaysIsHeld(t *testing.T) {
    fixture := newCommandFixture(t)
    userService := fixture.lazyUserService()

    /* the service reads the account once so its memo holds the roles as seeded, editor included */
    account, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")
    if _, _, grantErr := fixture.userRepository.GrantRole(context.Background(), account.Id, entity.RoleEditor); nil != grantErr {
        t.Fatalf("seed the editor role: %v", grantErr)
    }
    if _, _, findErr := userService.Get().FindByUsername("user"); nil != findErr {
        t.Fatalf("warm the memo: %v", findErr)
    }

    /* the directory drops the role behind the memo's back, through a door that dispatches nothing */
    account.Roles = []string{entity.RoleUser}
    if _, updateErr := fixture.userRepository.Update(context.Background(), account); nil != updateErr {
        t.Fatalf("remove the role in the directory: %v", updateErr)
    }

    if runErr := NewGrantRoleCommand(userService).Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user")); nil != runErr {
        t.Fatalf("the grant over a stale memo failed: %v", runErr)
    }

    if stored := storedRoles(t, fixture, "user"); false == slices.Contains(stored, entity.RoleEditor) {
        t.Fatalf("the cached read's word was taken over the directory's: the account holds %v", stored)
    }
}

/* the repository's own answer that the role is held is the one the command reports as nothing to do */
func TestGrantRoleCommandReportsTheRepositorysAlreadyHeld(t *testing.T) {
    fixture := newCommandFixture(t)

    account, _, _ := fixture.userRepository.FindByUsername(context.Background(), "user")
    if _, _, grantErr := fixture.userRepository.GrantRole(context.Background(), account.Id, entity.RoleEditor); nil != grantErr {
        t.Fatalf("seed the editor role: %v", grantErr)
    }

    previousStdout := os.Stdout
    reader, writer, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("open the capture pipe: %v", pipeErr)
    }
    os.Stdout = writer

    runErr := NewGrantRoleCommand(fixture.lazyUserService()).Run(fixture.runtime, newFlagContext(entity.RoleEditor, "user"))

    _ = writer.Close()
    os.Stdout = previousStdout

    captured := &bytes.Buffer{}
    _, _ = io.Copy(captured, reader)
    _ = reader.Close()

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

/* a grant whose write COMMITTED and whose listeners then refused is not a grant that failed: the role is in the
   directory, the cache entries of the account were not dropped, and the operator reads both — the output says
   the role was granted and names the listeners, the exit names the entries that stand, and a re-run would find
   the role held. A bare failure sent the operator to re-run a grant the re-run would find done. */
func TestGrantRoleCommandSaysTheRoleWasGrantedWhenTheListenersRefuse(t *testing.T) {
    fixture := newCommandFixtureWithDispatcher(t, func(dispatcher melodyeventcontract.EventDispatcher) melodyeventcontract.EventDispatcher {
        return &refusingDispatcher{EventDispatcher: dispatcher}
    })
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
