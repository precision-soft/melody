package migrate

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
)

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

func TestFinishFileAtomically_LeavesNoTemporaryBehindWhenTheRenameFails(t *testing.T) {
    directory := t.TempDir()
    destination := filepath.Join(directory, "20260826120000_create_users.go")

    if mkdirErr := os.Mkdir(destination, 0o700); nil != mkdirErr {
        t.Fatalf("could not seed a directory at the destination: %v", mkdirErr)
    }

    sentinel := filepath.Join(destination, "operator-owned")
    if writeErr := os.WriteFile(sentinel, []byte("keep this content"), 0o600); nil != writeErr {
        t.Fatal(writeErr)
    }

    finishErr := finishFileAtomically(destination, []byte("package migrations\n"))
    if nil == finishErr {
        t.Fatal("expected a refusal when the destination cannot be renamed over")
    }

    retained, readErr := os.ReadFile(sentinel)
    if nil != readErr || "keep this content" != string(retained) {
        t.Fatalf("failed finalization changed the destination: content=%q error=%v", retained, readErr)
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

func TestSyncDirectory_RefusesAPathThatIsNotThere(t *testing.T) {
    syncErr := syncDirectory(filepath.Join(t.TempDir(), "a directory that was never created"))
    if nil == syncErr {
        t.Fatal("expected a refusal for a directory that is not there")
    }

    if false == strings.Contains(syncErr.Error(), "fsync") {
        t.Fatalf("the refusal does not name what failed: %v", syncErr)
    }
}

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
