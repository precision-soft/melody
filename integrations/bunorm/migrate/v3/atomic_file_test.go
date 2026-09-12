package migrate

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
)

/* the whole point of the rewrite is that the destination ends up holding the WHOLE content, with nothing of the temporary neighbour left in the directory beside it — a leftover dot-file in a migrations directory is read by nothing, but it is also a lie about what the command did. */
func TestFinishFileAtomically_ReplacesTheContentAndLeavesNoTemporaryBehind(t *testing.T) {
    directory := t.TempDir()
    destination := filepath.Join(directory, "20260826120000_create_users.go")

    if writeErr := os.WriteFile(destination, []byte("truncated"), migrationFileMode); nil != writeErr {
        t.Fatalf("could not seed the destination: %v", writeErr)
    }

    content := "package migrations\n\nfunc init() {}\n"

    if finishErr := finishFileAtomically(destination, []byte(content)); nil != finishErr {
        t.Fatalf("unexpected error: %v", finishErr)
    }

    written, readErr := os.ReadFile(destination)
    if nil != readErr {
        t.Fatalf("could not read the destination: %v", readErr)
    }

    if content != string(written) {
        t.Fatalf("destination content = %q, want %q", string(written), content)
    }

    entries, readDirErr := os.ReadDir(directory)
    if nil != readDirErr {
        t.Fatalf("could not read the directory: %v", readDirErr)
    }

    if 1 != len(entries) {
        names := make([]string, 0, len(entries))
        for _, entry := range entries {
            names = append(names, entry.Name())
        }

        t.Fatalf("expected the destination alone in the directory, got %v", names)
    }
}

/* TestFinishFileAtomically_GivesTheDestinationTheMigrationMode is not a detail. os.CreateTemp makes its file 0600, and a rename carries the temporary file's mode onto the destination — so without the chmod the rewrite would silently narrow a world-readable migration to owner-only, and the narrowing would show up on a teammate's checkout rather than here. */
func TestFinishFileAtomically_GivesTheDestinationTheMigrationMode(t *testing.T) {
    directory := t.TempDir()
    destination := filepath.Join(directory, "20260826120000_create_users.go")

    if writeErr := os.WriteFile(destination, []byte("seed"), migrationFileMode); nil != writeErr {
        t.Fatalf("could not seed the destination: %v", writeErr)
    }

    if finishErr := finishFileAtomically(destination, []byte("package migrations\n")); nil != finishErr {
        t.Fatalf("unexpected error: %v", finishErr)
    }

    info, statErr := os.Stat(destination)
    if nil != statErr {
        t.Fatalf("could not stat the destination: %v", statErr)
    }

    if migrationFileMode != info.Mode().Perm() {
        t.Fatalf("destination mode = %v, want %v", info.Mode().Perm(), migrationFileMode)
    }
}

/* TestFinishFileAtomically_RefusesADestinationWhoseDirectoryIsNotThere pins the refusal by NAME rather than by panic, on a cause the environment cannot wave away. The obvious probe — a directory with the write bit cleared — is vacuous here: the development container runs its tests as root, and root ignores the permission bit, so that probe passed the failure straight through and reported a guard that had never run. */
func TestFinishFileAtomically_RefusesADestinationWhoseDirectoryIsNotThere(t *testing.T) {
    destination := filepath.Join(t.TempDir(), "a directory that was never created", "20260826120000_create_users.go")

    finishErr := finishFileAtomically(destination, []byte("package migrations\n"))
    if nil == finishErr {
        t.Fatal("expected a refusal for a directory that is not there")
    }

    if false == strings.Contains(finishErr.Error(), "temporary file") {
        t.Fatalf("the refusal does not name what failed: %v", finishErr)
    }
}

/* TestFinishFileAtomically_LeavesNoTemporaryBehindWhenTheRenameFails is the guard on the deferred cleanup, and it needs a failure that lands AFTER the temporary file exists — everything earlier fails before there is anything to clean up. A destination that is an existing DIRECTORY gives exactly that: the temporary neighbour is written, synced and chmodded, and only the rename refuses, because a file cannot be renamed over a directory.

   Without the cleanup the migrations directory keeps a dot-prefixed fragment of a migration for every failed attempt, which nothing reads and nothing removes. */
func TestFinishFileAtomically_LeavesNoTemporaryBehindWhenTheRenameFails(t *testing.T) {
    directory := t.TempDir()
    destination := filepath.Join(directory, "20260826120000_create_users.go")

    if mkdirErr := os.Mkdir(destination, 0o700); nil != mkdirErr {
        t.Fatalf("could not seed a directory at the destination: %v", mkdirErr)
    }

    finishErr := finishFileAtomically(destination, []byte("package migrations\n"))
    if nil == finishErr {
        t.Fatal("expected a refusal when the destination cannot be renamed over")
    }

    if false == strings.Contains(finishErr.Error(), "rename") {
        t.Fatalf("the refusal does not name what failed: %v", finishErr)
    }

    entries, readDirErr := os.ReadDir(directory)
    if nil != readDirErr {
        t.Fatalf("could not read the directory: %v", readDirErr)
    }

    if 1 != len(entries) {
        names := make([]string, 0, len(entries))
        for _, entry := range entries {
            names = append(names, entry.Name())
        }

        t.Fatalf("the failed rename left its temporary file behind: %v", names)
    }
}

/* the directory fsync is what makes the RENAME durable — the content can survive a crash while the directory entry naming it does not — so a missing directory is refused by name here rather than swallowed into a success. */
func TestSyncDirectory_RefusesAPathThatIsNotThere(t *testing.T) {
    syncErr := syncDirectory(filepath.Join(t.TempDir(), "a directory that was never created"))
    if nil == syncErr {
        t.Fatal("expected a refusal for a directory that is not there")
    }

    if false == strings.Contains(syncErr.Error(), "fsync") {
        t.Fatalf("the refusal does not name what failed: %v", syncErr)
    }
}

/* the rewrite keeps the mode the destination carries, which is bun's request filtered through the process umask: under umask 077 bun leaves 0600, and a rewrite that stamped 0644 unconditionally widened what the operator's umask had narrowed */
func TestFinishFileAtomically_KeepsTheModeTheDestinationCarries(t *testing.T) {
    previous := syscall.Umask(0o077)
    t.Cleanup(func() { syscall.Umask(previous) })

    directory := t.TempDir()
    destination := filepath.Join(directory, "20260905120000_create_users.go")

    if writeErr := os.WriteFile(destination, []byte("seed"), 0o644); nil != writeErr {
        t.Fatalf("could not seed the destination: %v", writeErr)
    }

    before, statErr := os.Stat(destination)
    if nil != statErr {
        t.Fatal(statErr)
    }

    if os.FileMode(0o600) != before.Mode().Perm() {
        t.Fatalf("control: the umask did not narrow the seed, mode = %v", before.Mode().Perm())
    }

    if finishErr := finishFileAtomically(destination, []byte("package migrations\n")); nil != finishErr {
        t.Fatalf("unexpected error: %v", finishErr)
    }

    after, statErr := os.Stat(destination)
    if nil != statErr {
        t.Fatal(statErr)
    }

    if os.FileMode(0o600) != after.Mode().Perm() {
        t.Fatalf("destination mode = %v, want the 0600 the umask left", after.Mode().Perm())
    }
}

/* a destination that is not there yet has no mode to keep, and bun's own 0644 is what it gets */
func TestFinishFileAtomically_FallsBackToBunsModeWhenTheDestinationIsNotThere(t *testing.T) {
    directory := t.TempDir()
    destination := filepath.Join(directory, "20260905120000_create_users.go")

    if finishErr := finishFileAtomically(destination, []byte("package migrations\n")); nil != finishErr {
        t.Fatalf("unexpected error: %v", finishErr)
    }

    info, statErr := os.Stat(destination)
    if nil != statErr {
        t.Fatal(statErr)
    }

    if migrationFileMode != info.Mode().Perm() {
        t.Fatalf("destination mode = %v, want %v", info.Mode().Perm(), migrationFileMode)
    }
}

/* the one failure that leaves the destination whole is marked as such, so the command can tell it from a rename that never landed */
func TestFinishFileAtomically_MarksADirectorySyncFailureAfterTheRename(t *testing.T) {
    previous := syncDirectoryAfterRename
    t.Cleanup(func() { syncDirectoryAfterRename = previous })
    syncDirectoryAfterRename = func(path string) error {
        return errors.New("fsync refused")
    }

    directory := t.TempDir()
    destination := filepath.Join(directory, "20260905120000_create_users.go")

    finishErr := finishFileAtomically(destination, []byte("package migrations\n"))
    if false == errors.Is(finishErr, errDirectorySyncAfterRename) {
        t.Fatalf("expected the directory sync marker, got %v", finishErr)
    }

    content, readErr := os.ReadFile(destination)
    if nil != readErr || "package migrations\n" != string(content) {
        t.Fatalf("expected the whole file in place beside the marked failure, got %q, %v", content, readErr)
    }
}
